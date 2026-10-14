import { useState, type FormEvent } from 'react';
import {
  ApiError,
  askKnowledge,
  fetchMaterial,
  searchKnowledge,
  type AskResponse,
  type Material,
  type Me,
  type RetrievalHit,
  type RetrievalMode,
} from './api';
import { AppHeader, type Theme } from './AppHeader';

interface Props {
  me: Me;
  theme: Theme;
  onToggleTheme: () => void;
  onLogout: () => void;
  onNavigate: (path: string) => void;
  onUnauthorized: () => void;
}

type Tab = 'search' | 'ask';

function errorText(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 401) return '登录已失效';
    if (err.status === 403) return '当前账号没有班级权限';
    if (err.code === 'model_not_configured') return '尚未配置真实对话模型，知识检索仍可正常使用';
    if (err.status === 503) return '向量或模型服务暂不可用，请稍后重试';
    if (err.status === 400) return '请输入有效的问题';
  }
  return '请求失败，请稍后重试';
}

export function Knowledge({ me, theme, onToggleTheme, onLogout, onNavigate, onUnauthorized }: Props) {
  const [tab, setTab] = useState<Tab>('search');
  const [query, setQuery] = useState('');
  const [mode, setMode] = useState<RetrievalMode>('hybrid');
  const [hits, setHits] = useState<RetrievalHit[]>([]);
  const [answer, setAnswer] = useState<AskResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [source, setSource] = useState<Material | null>(null);

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!query.trim() || loading) return;
    setLoading(true);
    setError('');
    try {
      if (tab === 'search') {
        const result = await searchKnowledge(query.trim(), mode, 4);
        setHits(result.hits);
        setAnswer(null);
      } else {
        const result = await askKnowledge(query.trim());
        setAnswer(result);
        setHits([]);
      }
    } catch (err) {
	  setHits([]);
	  setAnswer(null);
      if (err instanceof ApiError && err.status === 401) {
        onUnauthorized();
        return;
      }
      setError(errorText(err));
    } finally {
      setLoading(false);
    }
  };

  const openSource = async (materialId: string) => {
    setError('');
    try {
      setSource(await fetchMaterial(materialId));
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        onUnauthorized();
        return;
      }
      setError(err instanceof ApiError && err.status === 404 ? '来源不存在或无权访问' : errorText(err));
    }
  };

  return (
    <div className="app-shell">
      <AppHeader
        me={me}
        theme={theme}
        active="knowledge"
        onToggleTheme={onToggleTheme}
        onLogout={onLogout}
        onNavigate={onNavigate}
      />
      <main className="app-main knowledge-page">
        <section className="knowledge-hero">
          <div>
            <p className="eyebrow">本班知识库</p>
            <h1>可追溯知识检索</h1>
            <p>检索范围来自登录账号所属班级，每条结果都可以回到原材料核验。</p>
          </div>
          <div className="knowledge-tabs" role="tablist" aria-label="知识功能">
            <button type="button" role="tab" aria-selected={tab === 'search'} className={tab === 'search' ? 'active' : ''} onClick={() => setTab('search')}>知识检索</button>
            <button type="button" role="tab" aria-selected={tab === 'ask'} className={tab === 'ask' ? 'active' : ''} onClick={() => setTab('ask')}>知识问答</button>
          </div>
        </section>

        <form className="knowledge-form" onSubmit={submit}>
          <label className="field knowledge-query">
            <span className="field-label">{tab === 'search' ? '检索内容' : '向本班知识库提问'}</span>
            <textarea value={query} onChange={(e) => setQuery(e.target.value)} maxLength={2000} rows={3} placeholder={tab === 'search' ? '输入关键词或语义问题…' : '例如：第一讲介绍了什么？'} />
          </label>
          <div className="knowledge-form-actions">
            {tab === 'search' && (
              <label className="field mode-field">
                <span className="field-label">检索模式</span>
                <select value={mode} onChange={(e) => setMode(e.target.value as RetrievalMode)}>
                  <option value="hybrid">混合检索</option>
                  <option value="keyword">关键词</option>
                  <option value="vector">向量语义</option>
                </select>
              </label>
            )}
            <button className="btn btn-primary" type="submit" disabled={!query.trim() || loading}>
              {loading ? '处理中…' : tab === 'search' ? '检索' : '提问'}
            </button>
          </div>
        </form>

        {error && <div className="knowledge-error" role="alert">{error}</div>}

        {tab === 'search' && !loading && (
          <section className="knowledge-results" aria-live="polite">
            {hits.length === 0 ? <p className="knowledge-empty">暂无检索结果</p> : hits.map((hit) => (
              <HitCard key={hit.chunk_id} hit={hit} onOpen={() => openSource(hit.material_id)} />
            ))}
          </section>
        )}

        {tab === 'ask' && answer && (
          <section className="answer-panel" aria-live="polite">
            <h2>依据回答</h2>
            <p className={answer.citations.length === 0 ? 'no-evidence' : 'answer-text'}>{answer.answer}</p>
            {answer.citations.map((citation) => (
              <article className="citation-card" key={`${citation.ordinal}-${citation.chunk_id}`}>
                <div className="citation-title"><strong>[{citation.ordinal}] {citation.material_title}</strong><span>切片 {citation.chunk_index} · 字符 {citation.char_start}–{citation.char_end}</span></div>
                <p>{citation.excerpt}</p>
                <button type="button" className="btn btn-sm" onClick={() => openSource(citation.material_id)}>查看材料</button>
              </article>
            ))}
          </section>
        )}
      </main>

      {source && (
        <div className="detail-overlay" onMouseDown={(e) => e.target === e.currentTarget && setSource(null)}>
          <div className="detail-panel" role="dialog" aria-modal="true" aria-label={source.title}>
            <div className="detail-header"><h2>{source.title}</h2><button className="icon-btn" type="button" aria-label="关闭" onClick={() => setSource(null)}>×</button></div>
            <pre className="source-content">{source.content}</pre>
          </div>
        </div>
      )}
    </div>
  );
}

function HitCard({ hit, onOpen }: { hit: RetrievalHit; onOpen: () => void }) {
  return (
    <article className="retrieval-hit">
      <div className="hit-header">
        <h2>{hit.material_title}</h2>
        <span>切片 {hit.chunk_index} · 字符 {hit.char_start}–{hit.char_end}</span>
      </div>
      <p>{hit.excerpt}</p>
      <div className="hit-footer">
        <span>{hit.keyword_rank ? `关键词 #${hit.keyword_rank}` : ''}{hit.keyword_rank && hit.vector_rank ? ' · ' : ''}{hit.vector_rank ? `向量 #${hit.vector_rank}` : ''}{hit.final_rank ? ` · 综合 #${hit.final_rank}` : ''}</span>
        <button type="button" className="btn btn-sm" onClick={onOpen}>查看来源</button>
      </div>
    </article>
  );
}
