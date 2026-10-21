import { useCallback, useEffect, useRef, useState } from 'react';
import Markdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import {
  ApiError,
  createConversation,
  fetchAgents,
  type TutorAgent,
  downloadMaterial,
  fetchConversation,
  fetchConversations,
  fetchMaterial,
  getAccessToken,
  type Citation,
 type MCPCall,
  type Conversation,
  type ConversationMessage,
  type Me,
  type Material,
} from './api';
import { AppHeader, type Theme } from './AppHeader';
import { streamAssistantAnswer, StreamError } from './sse';
import { useToast } from './Toast';

const MAX_QUESTION_CHARS = 500;

interface Props {
  me: Me;
  theme: Theme;
  conversationId: string | null;
  onToggleTheme: () => void;
  onLogout: () => void;
  onNavigate: (path: string) => void;
  onOpenConversation: (id: string | null) => void;
  onUnauthorized: () => void;
}

// Local chat entry merges persisted history with optimistic in-flight turns.
interface ChatEntry {
  localId: string;
  id: string;
  role: 'user' | 'assistant';
  content: string;
  status: ConversationMessage['status'];
  errorCode?: string;
  citations?: Citation[];
  skillName?: string;
 toolCalls?: MCPCall[];
}

interface TurnError {
  code: string;
  message: string;
  retryable: boolean;
}

let localSeq = 1;
function nextLocalId(prefix: string): string {
  return `${prefix}-local-${localSeq++}`;
}

function errorText(code: string): string {
  switch (code) {
    case 'conversation_busy':
      return '上一轮回答尚未结束，请稍候。';
    case 'no_available_agents':
      return '本班暂无已启用智能体，请联系教师。';
    case 'retrieval_unavailable':
      return '资料检索暂时不可用，请稍后重试。';
    case 'model_unavailable':
      return 'AI 服务暂时不可用，请稍后重试。';
    case 'model_timeout':
      return '回答生成超时，请重试。';
    case 'output_limit_exceeded':
      return '回答超出长度限制，请尝试换一种问法。';
    case 'unsupported_citation':
      return '回答引用校验未通过，本轮已终止。';
    case 'missing_no_hit_disclosure':
      return '回答未说明本班资料未命中，本轮已终止。';
    case 'guided_violation':
      return '回答违反了解题引导规则，本轮已终止。';
    case 'stream_closed':
      return '连接中断，回答未完成。';
    case 'network_error':
      return '网络错误，请检查连接。';
    case 'invalid_question':
      return '问题不能为空且2–500 字。';
    default:
      return '回答未完成，请稍后重试。';
  }
}

function errorCodeText(code?: string): string {
  if (!code) return '生成失败';
  return `生成失败（${errorText(code)}）`;
}

function sanitizeUrl(url: string | undefined): string {
  const u = (url || '').trim();
  if (/^(javascript|vbscript|data):/i.test(u)) return '';
  return u;
}

export function Conversations({
  me,
  theme,
  conversationId,
  onToggleTheme,
  onLogout,
  onNavigate,
  onOpenConversation,
  onUnauthorized,
}: Props) {
  const toast = useToast();
  const [agents,setAgents]=useState<TutorAgent[]>([]);
  const [selectedAgent,setSelectedAgent]=useState<number>(0);
  const [agentLoadError,setAgentLoadError]=useState(false);
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [listLoading, setListLoading] = useState(true);
  const [detail, setDetail] = useState<Conversation | null>(null);
  const [entries, setEntries] = useState<ChatEntry[]>([]);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailError, setDetailError] = useState<TurnError | null>(null);
  const [draft, setDraft] = useState('');
  const [streaming, setStreaming] = useState(false);
  const [turnError, setTurnError] = useState<TurnError | null>(null);
  const [lastQuestion, setLastQuestion] = useState('');
  const [openMaterial, setOpenMaterial] = useState<Material | null>(null);
  const [materialLoading, setMaterialLoading] = useState(false);
  const [reloadKey, setReloadKey] = useState(0);
  const abortRef = useRef<AbortController | null>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const composerRef = useRef<HTMLTextAreaElement>(null);

  const loadList = useCallback(async () => {
    try {
      const list = await fetchConversations();
      setConversations(list);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) onUnauthorized();
      // List failures keep the current view usable; a retry happens on next visit.
    } finally {
      setListLoading(false);
    }
  }, [onUnauthorized]);

  useEffect(()=>{fetchAgents().then(list=>{const active=list.filter(a=>a.enabled);setAgents(active);setSelectedAgent(active[0]?.id??0);setAgentLoadError(false);}).catch(err=>{setAgentLoadError(true);if(err instanceof ApiError&&err.status===401)onUnauthorized();});},[onUnauthorized]);
  useEffect(() => {
    loadList();
  }, [loadList]);

  // Load (or clear) the selected conversation whenever the route changes.
  // Switching conversations aborts any in-flight stream so the composer never
  // stays locked by a turn the user has navigated away from.
  useEffect(() => {
    abortRef.current?.abort();
    abortRef.current = null;
    setStreaming(false);
    setTurnError(null);
    if (!conversationId) {
      setDetail(null);
      setEntries([]);
      return;
    }
    let cancelled = false;
    setDetailLoading(true);
    setDetailError(null);
    fetchConversation(conversationId)
      .then(({ conversation, messages }) => {
        if (cancelled) return;
        setDetail(conversation);
        setEntries(messages.map(toEntry));
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        if (err instanceof ApiError && err.status === 401) {
          onUnauthorized();
          return;
        }
        setDetail(null);
        setEntries([]);
        setDetailError({
          code: err instanceof ApiError ? `http_${err.status}` : 'network_error',
          message: '会话加载失败',
          retryable: true,
        });
      })
      .finally(() => {
        if (!cancelled) setDetailLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [conversationId, reloadKey, onUnauthorized]);

  // Keep the newest message visible while streaming.
  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
  }, [entries, streaming]);

  const handleNew = useCallback(async () => {
    try {
      if(!selectedAgent)return;
      const conv = await createConversation(selectedAgent);
      setConversations((prev) => [conv, ...prev]);
      onOpenConversation(conv.id);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        onUnauthorized();
        return;
      }
      toast.push('创建会话失败，请稍后重试', 'error');
    }
  }, [onOpenConversation, onUnauthorized, toast,selectedAgent]);

  const patchEntry = useCallback((localId: string, patch: Partial<ChatEntry>) => {
    setEntries((prev) => prev.map((e) => (e.localId === localId ? { ...e, ...patch } : e)));
  }, []);

  // appendDelta appends streaming text to the given assistant entry.
  const appendDelta = useCallback((localId: string, text: string) => {
    setEntries((prev) =>
      prev.map((e) => (e.localId === localId ? { ...e, content: e.content + text } : e)),
    );
  }, []);

  const runTurn = useCallback(
    async (question: string) => {
      if (!conversationId || streaming) return;
      const assistantLocalId = nextLocalId('a');
      const userLocalId = nextLocalId('u');
      setEntries((prev) => [
        ...prev,
        { localId: userLocalId, id: userLocalId, role: 'user', content: question, status: 'completed' },
        { localId: assistantLocalId, id: assistantLocalId, role: 'assistant', content: '', status: 'streaming' },
      ]);
      setStreaming(true);
      setTurnError(null);
      setLastQuestion(question);

      const controller = new AbortController();
      abortRef.current = controller;

      try {
        const terminal = await streamAssistantAnswer({
          conversationId: conversationId,
          question,
          token: getAccessToken() ?? '',
          signal: controller.signal,
          onEvent: (e) => {
            if (e.type === 'meta') {
              patchEntry(assistantLocalId, { id: e.assistant_message_id, citations:e.citations, skillName:e.skill?.name,toolCalls:e.tool_calls });
            } else if (e.type === 'delta') {
              appendDelta(assistantLocalId, e.text);

            }
          },
        });
        if (terminal.status === 'completed') {
          patchEntry(assistantLocalId, { status: 'completed', citations: terminal.citations });
        } else {
          patchEntry(assistantLocalId, {
            status: 'failed',
            errorCode: terminal.code,
            citations: [],
          });
          setTurnError({ code: terminal.code, message: errorText(terminal.code), retryable: terminal.retryable });
        }
      } catch (err) {
        if (err instanceof StreamError && err.status === 401) {
          onUnauthorized();
          return;
        }
        if (err instanceof DOMException && err.name === 'AbortError') {
          patchEntry(assistantLocalId, { status: 'cancelled', citations: [] });
          toast.push('已停止生成', 'info');
        } else if (err instanceof StreamError && err.status === 409) {
          // Pre-stream rejection: roll back optimistic messages entirely and
          // give the draft back so the question is not lost.
          setEntries((prev) => prev.filter((e) => e.localId !== userLocalId && e.localId !== assistantLocalId));
          setDraft(question);
          setTurnError({ code: err.code, message: errorText(err.code), retryable: false });
        } else if (err instanceof StreamError) {
          patchEntry(assistantLocalId, { status: 'failed', errorCode: err.code, citations: [] });
          setTurnError({ code: err.code, message: errorText(err.code), retryable: err.retryable });
        } else {
          patchEntry(assistantLocalId, { status: 'failed', errorCode: 'internal', citations: [] });
          setTurnError({ code: 'internal', message: errorText('internal'), retryable: true });
        }
      } finally {
        abortRef.current = null;
        setStreaming(false);
        loadList();
      }
    },
    [conversationId, streaming, patchEntry, appendDelta, setDraft, onUnauthorized, toast, loadList],
  );

  const handleSend = useCallback(() => {
    const question = draft.trim();
    if (!conversationId || streaming) return;
    if ([...question].length < 2 || [...question].length > MAX_QUESTION_CHARS) return;
    setDraft('');
    runTurn(question);
  }, [draft, conversationId, streaming, runTurn]);

  const handleStop = useCallback(() => {
    abortRef.current?.abort();
  }, []);

  const handleRetry = useCallback(() => {
    if (!lastQuestion || streaming) return;
    runTurn(lastQuestion);
  }, [lastQuestion, streaming, runTurn]);

  const handleOpenMaterial = useCallback(
    async (citation: Citation) => {
      setMaterialLoading(true);
      try {
        const m = await fetchMaterial(citation.material_id);
        setOpenMaterial(m);
      } catch (err) {
        if (err instanceof ApiError && err.status === 401) {
          onUnauthorized();
          return;
        }
        toast.push('无法打开材料，请稍后重试', 'error');
      } finally {
        setMaterialLoading(false);
      }
    },
    [onUnauthorized, toast],
  );

  const handleDownload = useCallback(
    async (m: Material) => {
      try {
        const blob = await downloadMaterial(m.id);
        const url = URL.createObjectURL(blob);
        const link = document.createElement('a');
        link.href = url;
        link.download = m.original_filename || m.title;
        document.body.appendChild(link);
        link.click();
        link.remove();
        window.setTimeout(() => URL.revokeObjectURL(url), 1000);
      } catch (err) {
        if (err instanceof ApiError && err.status === 401) {
          onUnauthorized();
          return;
        }
        toast.push('下载失败，请稍后重试', 'error');
      }
    },
    [onUnauthorized, toast],
  );

  const draftLen = [...draft].length;
  const canSend = !streaming && [...draft.trim()].length >= 2 && draftLen <= MAX_QUESTION_CHARS && !!conversationId;

  return (
    <div className="app-shell">
      <AppHeader
        me={me}
        theme={theme}
        active="conversations"
        onToggleTheme={onToggleTheme}
        onLogout={onLogout}
        onNavigate={onNavigate}
      />
      <div className="convo-layout">
        <aside className={`convo-sidebar${conversationId ? ' collapsed' : ''}`} aria-label="会话列表">
          <div className="convo-sidebar-head">
            <button type="button" className="btn btn-primary btn-sm" onClick={handleNew} disabled={!selectedAgent}>
              ＋ 新对话
            </button>
          </div>
          <div className="agent-picker">
            <label htmlFor="agent-select">新会话使用的智能体</label>
            <select id="agent-select" value={selectedAgent} onChange={e=>setSelectedAgent(Number(e.target.value))} disabled={streaming}>
              {agents.map(a=><option key={a.id} value={a.id}>{a.name}</option>)}
            </select>
            {agentLoadError?<p role="alert">智能体加载失败，请刷新重试。</p>:agents.length===0?<p>本班暂无已启用智能体，请联系教师。</p>:<p>会话创建后固定智能体，换助手请新建会话。</p>}
          </div>
          {listLoading ? (
            <div className="convo-list-status" role="status">
              加载中…
            </div>
          ) : conversations.length === 0 ? (
            <div className="convo-list-status">还没有会话，点击「新对话」开始提问。</div>
          ) : (
            <ul className="convo-list" role="list">
              {conversations.map((c) => (
                <li key={c.id}>
                  <button
                    type="button"
                    className={`convo-item${c.id === conversationId ? ' active' : ''}`}
                    aria-current={c.id === conversationId ? 'true' : undefined}
                    onClick={() => onOpenConversation(c.id)}
                  >
                    <span className="convo-item-title">{c.title}</span>
                    <span className="convo-item-time">{formatTime(c.updated_at)}</span>
                  </button>
                </li>
              ))}
            </ul>
          )}
        </aside>

        <main className="chat-pane" aria-label="对话">
          {conversationId === null ? (
            <div className="chat-placeholder">
              <h2>AI 学习助手</h2>
              <p>选择左侧会话继续追问，或创建新对话开始提问。助手先检索已绑定的本班资料；找到资料时显示引用出处。</p>
            </div>
          ) : (
            <>
              <div className="chat-head">
                <button
                  type="button"
                  className="btn btn-sm chat-back"
                  onClick={() => onOpenConversation(null)}
                  aria-label="返回会话列表"
                >
                  ← 会话
                </button>
                <h2 className="chat-title">{detail?.title ?? '…'}</h2>
              </div>
              <div ref={scrollRef} className="chat-messages">
                {detailLoading && entries.length === 0 && (
                  <div className="convo-list-status" role="status" aria-live="polite">
                    <span className="spinner" aria-hidden="true" /> 加载中…
                  </div>
                )}
                {detailError && !detailLoading && (
                  <div className="materials-error" role="alert">
                    <h3>会话加载失败</h3>
                    <button type="button" className="btn btn-sm" onClick={() => setReloadKey((k) => k + 1)}>
                      重试
                    </button>
                  </div>
                )}
                {entries.map((e) => (
                  <MessageBubble key={e.localId} entry={e} onOpenMaterial={handleOpenMaterial} materialLoading={materialLoading} />
                ))}
                {entries.length === 0 && !detailLoading && !detailError && (
                  <div className="chat-placeholder">
                    <p>还没有消息，输入第一个问题吧。</p>
                  </div>
                )}
              </div>
              <p className="agent-context">当前智能体：{detail?.agent_name || "加载中"}</p>
              {/* Polite live region announcing turn progress for screen readers. */}
              <p className="visually-hidden" role="status" aria-live="polite">
                {streaming ? '正在生成回答…' : turnError ? `回答未完成：${turnError.message}` : entries.length === 0 ? '等待提问' : entries.at(-1)?.status === 'cancelled' ? '已停止生成' : '回答已完成'}
              </p>
              {turnError && (
                <div className="turn-error" role="alert">
                  <span>{turnError.message}</span>
                  {turnError.retryable && (
                    <button type="button" className="btn btn-sm" onClick={handleRetry} disabled={streaming}>
                      重试
                    </button>
                  )}
                </div>
              )}
              <div className="composer">
                <label className="visually-hidden" htmlFor="question-input">
                  输入你的问题
                </label>
                <textarea
                  id="question-input"
                  ref={composerRef}
                  className="composer-input"
                  value={draft}
                  onChange={(e) => setDraft(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
                      e.preventDefault();
                      handleSend();
                    }
                  }}
                  rows={2}
                  placeholder="输入你的问题…（Ctrl/⌘+Enter 发送）"
                  aria-describedby="question-counter"
                  disabled={streaming}
                />
                <div className="composer-actions">
                  <span id="question-counter" className={`char-counter${draftLen > MAX_QUESTION_CHARS ? ' over' : ''}`}>
                    {draftLen} / {MAX_QUESTION_CHARS}
                  </span>
                  {streaming ? (
                    <button type="button" className="btn btn-danger" onClick={handleStop}>
                      停止生成
                    </button>
                  ) : (
                    <button type="button" className="btn btn-primary" onClick={handleSend} disabled={!canSend}>
                      发送
                    </button>
                  )}
                </div>
              </div>
            </>
          )}
        </main>
      </div>

      {openMaterial && (
        <div
          className="detail-overlay"
          onMouseDown={(e) => {
            if (e.target === e.currentTarget) setOpenMaterial(null);
          }}
        >
          <div className="detail-panel" role="dialog" aria-modal="true" aria-label={openMaterial.title}>
            <div className="detail-header">
              <h2 title={openMaterial.title}>{openMaterial.title}</h2>
              <button type="button" className="icon-btn" aria-label="关闭" onClick={() => setOpenMaterial(null)}>
                ×
              </button>
            </div>
            <div className="detail-body">
              <div className="detail-meta">
                <div>
                  <span className="detail-meta-label">上传者：</span>
                  {openMaterial.uploader}
                </div>
                <div>
                  <span className="detail-meta-label">时间：</span>
                  {formatTime(openMaterial.created_at)}
                </div>
              </div>
              <pre className="detail-text">{openMaterial.content || ''}</pre>
            </div>
            <div className="detail-footer">
              <button type="button" className="btn btn-primary" onClick={() => handleDownload(openMaterial)}>
                下载文件
              </button>
              <button type="button" className="btn" onClick={() => setOpenMaterial(null)}>
                关闭
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function toEntry(m: ConversationMessage): ChatEntry {
  return { localId: m.id, id: m.id, role: m.role, content: m.content, status: m.status, errorCode: m.error_code, citations: m.citations, skillName:m.skill_name,toolCalls:m.tool_calls };
}

function MessageBubble({
  entry,
  onOpenMaterial,
  materialLoading,
}: {
  entry: ChatEntry;
  onOpenMaterial: (c: Citation) => void;
  materialLoading: boolean;
}) {
  const citationMap = new Map<number, Citation>();
  for (const c of entry.citations ?? []) citationMap.set(c.ordinal, c);
  return (
    <div className={`msg msg-${entry.role}`}>
      <div className={`msg-bubble${entry.status === 'failed' ? ' failed' : ''}`}>
        {entry.role==='assistant'&&entry.skillName&&<p className="msg-skill">本轮技能：{entry.skillName}</p>}
        {entry.role==='assistant'&&entry.toolCalls?.map((call,i)=><div className="mcp-call" key={`${i}-${call.ref}`}><strong>{call.ref}</strong> · 第{call.round}轮 · {({ok:'成功',denied:'被拒',error:'出错'})[call.status]}<p>{call.text}</p></div>)}
        {entry.role === 'assistant' ? (
          <div className="markdown">
            <Markdown remarkPlugins={[remarkGfm]} urlTransform={sanitizeUrl} skipHtml>
              {entry.content || (entry.status === 'streaming' ? '…' : '')}
            </Markdown>
          </div>
        ) : (
          <span className="msg-text">{entry.content}</span>
        )}
        {entry.status === 'streaming' && <span className="streaming-cursor" aria-hidden="true" />}
        {entry.status === 'failed' && <p className="msg-status failed">{errorCodeText(entry.errorCode)}</p>}
        {entry.status === 'cancelled' && <p className="msg-status">已停止生成</p>}
      </div>
      {entry.role === 'assistant' && (entry.status === 'completed' || entry.status === 'streaming') && (
        <div className="msg-citations">
          {(entry.citations ?? []).length === 0 ? (
            <span className="no-citation-note">本回答无引用来源</span>
          ) : (
            (entry.citations ?? []).map((c) => (
              <div key={c.ordinal} className="citation-card">
                <div className="citation-head">
                  <span className="citation-ordinal">[{c.ordinal}]</span>
                  <span className="citation-title">{c.material_title}</span>
                  <span className="citation-loc">
                    片段 {c.chunk_index + 1}
                    {c.char_end > c.char_start ? `（字符 ${c.char_start}–${c.char_end}）` : ''}
                  </span>
                </div>
                {c.excerpt && <blockquote className="citation-excerpt">{c.excerpt}</blockquote>}
                <button
                  type="button"
                  className="btn btn-sm citation-open"
                  onClick={() => onOpenMaterial(c)}
                  disabled={materialLoading}
                >
                  查看材料
                </button>
              </div>
            ))
          )}
        </div>
      )}
    </div>
  );
}

function formatTime(iso: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}
