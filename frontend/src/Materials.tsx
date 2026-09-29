import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import Markdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import {
  ApiError,
  fetchMaterials,
  materialFileUrl,
  type Material,
  type Me,
} from './api';
import { useToast } from './Toast';
import { Upload } from './Upload';
import type { Command } from './CommandPalette';

type Theme = 'light' | 'dark';
type ViewPref = 'list' | 'grid';
type ErrorKind = 'forbidden' | 'not_found' | 'unavailable' | 'error' | 'network';

interface Props {
  me: Me;
  theme: Theme;
  view: ViewPref;
  onToggleTheme: () => void;
  onToggleView: () => void;
  onLogout: () => void;
  onUnauthorized: () => void;
  setPaletteCommands: (cmds: Command[]) => void;
}

// Reject dangerous URL schemes in rendered markdown.
function sanitizeUrl(url: string | undefined): string {
  const u = (url || '').trim();
  if (/^(javascript|vbscript|data):/i.test(u)) return '';
  return u;
}

function extOf(name: string): string {
  const i = name.lastIndexOf('.');
  return i >= 0 ? name.slice(i + 1).toLowerCase() : '';
}

function isMarkdown(name: string): boolean {
  return ['md', 'markdown'].includes(extOf(name));
}

function isText(name: string): boolean {
  return ['txt', 'log', 'text'].includes(extOf(name)) || isMarkdown(name);
}

export function Materials({
  me,
  theme,
  view,
  onToggleTheme,
  onToggleView,
  onLogout,
  onUnauthorized,
  setPaletteCommands,
}: Props) {
  const toast = useToast();
  const [materials, setMaterials] = useState<Material[]>([]);
  const [loading, setLoading] = useState(true);
  const [errorKind, setErrorKind] = useState<ErrorKind | null>(null);
  const [search, setSearch] = useState('');
  const [selected, setSelected] = useState<Material | null>(null);
  const [uploadOpen, setUploadOpen] = useState(false);
  const searchRef = useRef<HTMLInputElement>(null);

  // Load the materials list, branching on error status per spec.
  const load = useCallback(
    async (q: string) => {
      setLoading(true);
      setErrorKind(null);
      try {
        const data = await fetchMaterials(q);
        setMaterials(data);
      } catch (err) {
        if (err instanceof ApiError) {
          if (err.status === 401) {
            onUnauthorized();
            return;
          }
          if (err.status === 403) setErrorKind('forbidden');
          else if (err.status === 503) setErrorKind('unavailable');
          else setErrorKind('error');
        } else {
          setErrorKind('network');
        }
      } finally {
        setLoading(false);
      }
    },
    [onUnauthorized],
  );

  // Debounced live search; the same effect handles the initial load.
  useEffect(() => {
    const t = window.setTimeout(() => load(search), 250);
    return () => window.clearTimeout(t);
  }, [search, load]);

  // Refresh after upload success.
  const refresh = useCallback(() => {
    load(search);
  }, [load, search]);

  const openMaterial = useCallback((m: Material) => {
    setSelected(m);
  }, []);

  const closeDetail = useCallback(() => setSelected(null), []);

  // Close detail on Escape.
  useEffect(() => {
    if (!selected) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') closeDetail();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [selected, closeDetail]);

  // Register page-specific commands with the global palette.
  useEffect(() => {
    const cmds: Command[] = [
      {
        id: 'search',
        label: '搜索资料',
        hint: '聚焦搜索框',
        skipFocusRestore: true,
        run: () => {
          searchRef.current?.focus();
          searchRef.current?.select();
        },
      },
      {
        id: 'theme',
        label: theme === 'light' ? '切换到深色主题' : '切换到浅色主题',
        keywords: 'theme toggle dark light',
        run: onToggleTheme,
      },
      {
        id: 'view',
        label: view === 'list' ? '切换到网格视图' : '切换到列表视图',
        keywords: 'view grid list toggle',
        run: onToggleView,
      },
    ];
    if (me.role === 'teacher') {
      cmds.push({
        id: 'upload',
        label: '上传资料',
        run: () => setUploadOpen(true),
      });
    }
    for (const m of materials) {
      cmds.push({
        id: `open-${m.id}`,
        label: m.title,
        hint: '打开资料',
        keywords: `${m.original_filename} ${m.uploader} ${m.title}`,
        run: () => openMaterial(m),
      });
    }
    setPaletteCommands(cmds);
  }, [
    materials,
    theme,
    view,
    me.role,
    onToggleTheme,
    onToggleView,
    openMaterial,
    setPaletteCommands,
  ]);

  const body = useMemo(() => {
    if (loading && materials.length === 0) {
      return (
        <div className="materials-empty" role="status" aria-live="polite">
          <span className="spinner" />
          <h3>加载中…</h3>
        </div>
      );
    }
    if (errorKind) {
      return (
        <div className="materials-error" role="alert">
          <h3>{errorTitle(errorKind)}</h3>
          <p>{errorMessage(errorKind)}</p>
          <button
            type="button"
            className="btn btn-sm"
            onClick={() => load(search)}
          >
            重试
          </button>
        </div>
      );
    }
    if (materials.length === 0) {
      return (
        <div className="materials-empty">
          <h3>暂无资料</h3>
          <p>
            {search
              ? `没有匹配「${search}」的资料`
              : '本班级还没有上传任何资料。'}
          </p>
        </div>
      );
    }
    const containerClass = view === 'grid' ? 'materials-grid' : 'materials-list';
    return (
      <div className={containerClass} role="list">
        {materials.map((m) => (
          <button
            key={m.id}
            type="button"
            className="material-card"
            role="listitem"
            onClick={() => openMaterial(m)}
            aria-label={`打开资料 ${m.title}`}
          >
            <span className="material-icon" aria-hidden="true">
              {extOf(m.original_filename) || 'TXT'}
            </span>
            <span className="material-body">
              <span className="material-title">{m.title}</span>
              <span className="material-meta">
                <span className="material-uploader">{m.uploader}</span>
                <span>{formatDate(m.created_at)}</span>
                {m.original_filename && (
                  <span>{m.original_filename}</span>
                )}
              </span>
            </span>
          </button>
        ))}
      </div>
    );
  }, [loading, materials, errorKind, view, search, openMaterial, load]);

  return (
    <div className="app-shell">
      <header className="app-header">
        <div className="app-brand">
          <span className="brand-mark" aria-hidden="true">C</span>
          <span className="brand-name">CampusClaw</span>
        </div>
        <span className="header-user" title={me.username}>
          <span>{me.username}</span>
          <span className="role-tag">
            {me.role === 'teacher' ? '教师' : '学生'}
          </span>
        </span>
        <div className="header-actions">
          {me.role === 'teacher' && (
            <button
              type="button"
              className="btn btn-primary btn-sm"
              onClick={() => setUploadOpen(true)}
            >
              <span className="btn-label">上传</span>
            </button>
          )}
          <button
            type="button"
            className="btn btn-sm"
            onClick={onToggleTheme}
            aria-label={
              theme === 'light' ? '切换到深色主题' : '切换到浅色主题'
            }
            title={theme === 'light' ? '深色主题' : '浅色主题'}
          >
            {theme === 'light' ? '🌙' : '☀'}
          </button>
          <div
            className="view-toggle"
            role="group"
            aria-label="视图切换"
          >
            <button
              type="button"
              className={view === 'list' ? 'active' : ''}
              onClick={() => view !== 'list' && onToggleView()}
              aria-pressed={view === 'list'}
            >
              列表
            </button>
            <button
              type="button"
              className={view === 'grid' ? 'active' : ''}
              onClick={() => view !== 'grid' && onToggleView()}
              aria-pressed={view === 'grid'}
            >
              网格
            </button>
          </div>
          <button
            type="button"
            className="btn btn-sm"
            onClick={onLogout}
          >
            退出
          </button>
        </div>
      </header>

      <main className="app-main">
        <div className="materials-toolbar">
          <div className="search-box">
            <span className="search-icon" aria-hidden="true">🔍</span>
            <input
              ref={searchRef}
              className="search-input"
              type="search"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="搜索资料标题或内容…"
              aria-label="搜索资料"
            />
          </div>
          <button
            type="button"
            className="btn btn-sm"
            onClick={() => onToggleView()}
            aria-label={
              view === 'list' ? '切换到网格视图' : '切换到列表视图'
            }
            title="切换视图"
          >
            {view === 'list' ? '▦' : '≣'}
          </button>
        </div>
        {body}
      </main>

      {selected && (
        <div
          className="detail-overlay"
          onMouseDown={(e) => {
            if (e.target === e.currentTarget) closeDetail();
          }}
        >
          <div className="detail-panel" role="dialog" aria-modal="true" aria-label={selected.title}>
            <div className="detail-header">
              <h2 title={selected.title}>{selected.title}</h2>
              <button
                type="button"
                className="icon-btn"
                aria-label="关闭"
                onClick={closeDetail}
              >
                ×
              </button>
            </div>
            <div className="detail-body">
              <div className="detail-meta">
                <div>
                  <span className="detail-meta-label">上传者：</span>
                  {selected.uploader}
                </div>
                <div>
                  <span className="detail-meta-label">时间：</span>
                  {formatDate(selected.created_at)}
                </div>
                {selected.original_filename && (
                  <div>
                    <span className="detail-meta-label">文件：</span>
                    {selected.original_filename}
                  </div>
                )}
              </div>
              <MaterialContent material={selected} />
            </div>
            <div className="detail-footer">
              <a
                className="btn btn-primary"
                href={materialFileUrl(selected.id)}
                download={selected.original_filename || undefined}
              >
                下载文件
              </a>
              <button type="button" className="btn" onClick={closeDetail}>
                关闭
              </button>
            </div>
          </div>
        </div>
      )}

      {uploadOpen && me.role === 'teacher' && (
        <Upload
          onClose={() => setUploadOpen(false)}
          onUploaded={refresh}
          onUnauthorized={onUnauthorized}
          onToast={(msg, kind) => toast.push(msg, kind)}
        />
      )}
    </div>
  );
}

function MaterialContent({ material }: { material: Material }) {
  const name = material.original_filename;
  if (isMarkdown(name)) {
    return (
      <div className="markdown">
        <Markdown
          remarkPlugins={[remarkGfm]}
          urlTransform={sanitizeUrl}
          skipHtml
        >
          {material.content || ''}
        </Markdown>
      </div>
    );
  }
  if (isText(name)) {
    return (
      <pre className="detail-text">{material.content || ''}</pre>
    );
  }
  // Unknown / binary: show whatever text we have, or a hint.
  if (material.content) {
    return <pre className="detail-text">{material.content}</pre>;
  }
  return (
    <div className="materials-empty">
      <p>此文件类型不支持预览，请下载查看。</p>
    </div>
  );
}

function errorTitle(k: ErrorKind): string {
  switch (k) {
    case 'forbidden':
      return '无访问权限';
    case 'not_found':
      return '未找到';
    case 'unavailable':
      return '服务暂不可用';
    case 'network':
      return '网络错误';
    default:
      return '请求失败';
  }
}

function errorMessage(k: ErrorKind): string {
  switch (k) {
    case 'forbidden':
      return '你没有权限查看此班级的资料。';
    case 'not_found':
      return '请求的资料不存在。';
    case 'unavailable':
      return '服务暂时不可用，请稍后重试。';
    case 'network':
      return '无法连接到服务器，请检查网络。';
    default:
      return '加载资料时出错。';
  }
}

function formatDate(iso: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}
