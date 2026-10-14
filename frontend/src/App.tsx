import { useCallback, useEffect, useState, type ReactNode } from 'react';
import {
  ApiError,
  clearAccessToken,
  fetchMe,
  login as apiLogin,
  logout as apiLogout,
  type Me,
} from './api';
import { AssistantSettings } from './AssistantSettings';
import { Conversations } from './Conversations';
import { CommandPalette, type Command } from './CommandPalette';
import { Login } from './Login';
import { Materials } from './Materials';
import { Knowledge } from './Knowledge';
import { ToastProvider } from './Toast';

type Theme = 'light' | 'dark';
type ViewPref = 'list' | 'grid';

type AppRoute =
  | { name: 'loading' }
  | { name: 'login' }
  | { name: 'unavailable' }
  | { name: 'materials' }
	| { name: 'knowledge' }
  | { name: 'assistant' }
  | { name: 'conversations'; conversationId: string | null };

const THEME_KEY = 'campusclaw-theme';
const VIEW_KEY = 'campusclaw-view';

function getStoredTheme(): Theme {
  try {
    const t = localStorage.getItem(THEME_KEY);
    if (t === 'light' || t === 'dark') return t;
  } catch {
    /* ignore */
  }
  // First visit: follow system preference.
  const prefersDark =
    typeof window.matchMedia === 'function' &&
    window.matchMedia('(prefers-color-scheme: dark)').matches;
  return prefersDark ? 'dark' : 'light';
}

function getStoredView(): ViewPref {
  try {
    const v = localStorage.getItem(VIEW_KEY);
    if (v === 'list' || v === 'grid') return v;
  } catch {
    /* ignore */
  }
  return 'list';
}

function applyTheme(theme: Theme) {
  document.documentElement.setAttribute('data-theme', theme);
}

function currentPath(): string {
  return window.location.pathname;
}

// parseRoute maps a URL path to an app route. Unknown paths fall back to
// materials; role guards are applied separately once identity is known.
function parseRoute(path: string): AppRoute {
  if (path === '/materials' || path === '/') return { name: 'materials' };
	if (path === '/knowledge') return { name: 'knowledge' };
  if (path === '/assistant') return { name: 'assistant' };
  if (path === '/conversations') return { name: 'conversations', conversationId: null };
  const match = /^\/conversations\/([^/]+)$/.exec(path);
  if (match) return { name: 'conversations', conversationId: decodeURIComponent(match[1]) };
  return { name: 'materials' };
}

function pathForRoute(route: AppRoute): string {
  switch (route.name) {
    case 'assistant':
      return '/assistant';
	case 'knowledge':
		return '/knowledge';
    case 'conversations':
      return route.conversationId ? `/conversations/${route.conversationId}` : '/conversations';
    default:
      return '/materials';
  }
}

// Update the URL without dispatching a synthetic popstate. Route state is set
// directly by callers; the popstate listener only reacts to real back/forward
// navigation (after state has flushed).
function pushPath(path: string) {
  if (path !== window.location.pathname) {
    history.pushState({}, '', path);
  }
}

export function App() {
  const [route, setRoute] = useState<AppRoute>({ name: 'loading' });
  const [me, setMe] = useState<Me | null>(null);
  const [theme, setTheme] = useState<Theme>(getStoredTheme);
  const [view, setView] = useState<ViewPref>(getStoredView);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [paletteCommands, setPaletteCommands] = useState<Command[]>([]);

  // Apply theme to <html> whenever it changes.
  useEffect(() => {
    applyTheme(theme);
  }, [theme]);

  // Persist view preference (NOT session/role/materials).
  useEffect(() => {
    try {
      localStorage.setItem(VIEW_KEY, view);
    } catch {
      /* ignore */
    }
  }, [view]);

  // Identity check on mount: 200 -> route parsed from the URL (identity
  // recovery on refresh), 401 -> /login (no session clear), 503 -> service
  // unavailable WITHOUT clearing session.
  const checkIdentity = useCallback(async () => {
    setRoute({ name: 'loading' });
    try {
      const identity = await fetchMe();
      setMe(identity);
      const path = currentPath();
      if (path === '/login' || path === '') {
        const home = identity.role === 'student' ? '/conversations' : '/materials';
        pushPath(home);
        setRoute(parseRoute(home));
        return;
      }
      setRoute(parseRoute(path));
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.status === 401) {
          clearAccessToken();
          setMe(null);
          setPaletteCommands([]);
          const path = currentPath();
          if (path !== '/login') {
            pushPath('/login');
          }
          setRoute({ name: 'login' });
        } else if (err.status === 503 || err.status === 0) {
          // Service unavailable or unreachable: do NOT clear session.
          setRoute({ name: 'unavailable' });
        } else {
          // Treat other unexpected statuses like unavailable without logout.
          setRoute({ name: 'unavailable' });
        }
      } else {
        setRoute({ name: 'unavailable' });
      }
    }
  }, []);

  useEffect(() => {
    checkIdentity();
  }, [checkIdentity]);

  // Role guard: redirect unauthorized pages to materials after identity loads.
  useEffect(() => {
    if (!me) return;
    if (me.role === 'student' && route.name === 'knowledge') {
      pushPath('/conversations');
      setRoute({ name: 'conversations', conversationId: null });
    }
    if (me.role === 'student' && route.name === 'assistant') {
      pushPath('/materials');
      setRoute({ name: 'materials' });
    }
  }, [me, route]);

  // Respond to back/forward: keep route in sync with the URL.
  useEffect(() => {
    const onPop = () => {
      if (!me) return;
      const path = currentPath();
      if (path === '/login') {
        setRoute({ name: 'login' });
        return;
      }
      setRoute(parseRoute(path));
    };
    window.addEventListener('popstate', onPop);
    return () => window.removeEventListener('popstate', onPop);
  }, [me]);

  // Global Cmd/Ctrl+K toggles the palette.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && (e.key === 'k' || e.key === 'K')) {
        e.preventDefault();
        setPaletteOpen((o) => !o);
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  const navigate = useCallback((path: string) => {
    pushPath(path);
    setRoute(parseRoute(path));
  }, []);

  const handleLogin = useCallback(async (username: string, password: string) => {
    await apiLogin(username, password);
    const identity = await fetchMe();
    setMe(identity);
    const home = identity.role === 'student' ? '/conversations' : '/materials';
    pushPath(home);
    setRoute(parseRoute(home));
  }, []);

  const handleLogout = useCallback(async () => {
    try {
      await apiLogout();
    } catch {
      // Even if logout call fails, drop local state and go to login.
    }
    setMe(null);
    setPaletteCommands([]);
    pushPath('/login');
    setRoute({ name: 'login' });
  }, []);

  const onUnauthorized = useCallback(() => {
    clearAccessToken();
    // 401 on a business request: clear local state and redirect to /login.
    setMe(null);
    setPaletteCommands([]);
    pushPath('/login');
    setRoute({ name: 'login' });
  }, []);

  const toggleTheme = useCallback(() => {
    setTheme((t) => {
      const next = t === 'light' ? 'dark' : 'light';
      try {
        localStorage.setItem(THEME_KEY, next);
      } catch {
        /* ignore */
      }
      return next;
    });
  }, []);

  const toggleView = useCallback(() => {
    setView((v) => (v === 'list' ? 'grid' : 'list'));
  }, []);

  // Palette commands depend on route; on login only theme is relevant.
  useEffect(() => {
    if (route.name === 'login') {
      setPaletteCommands([
        {
          id: 'theme',
          label: theme === 'light' ? '切换到深色主题' : '切换到浅色主题',
          keywords: 'theme toggle dark light',
          run: toggleTheme,
        },
      ]);
    }
    // Pages register their own commands via setPaletteCommands.
  }, [route.name, theme, toggleTheme]);

  const paletteEl = (
    <CommandPalette
      open={paletteOpen}
      onClose={() => setPaletteOpen(false)}
      commands={paletteCommands}
    />
  );

  let content: ReactNode;
  if (route.name === 'loading') {
    content = (
      <main className="status-page" role="status" aria-live="polite">
        <div className="status-card">
          <span className="spinner" />
          <p>正在加载…</p>
        </div>
      </main>
    );
  } else if (route.name === 'unavailable') {
    content = (
      <main className="status-page">
        <div className="status-card">
          <h1>服务暂不可用</h1>
          <p role="alert">暂时无法连接到服务，请稍后重试。</p>
          <button
            type="button"
            className="btn btn-primary"
            onClick={checkIdentity}
          >
            重试
          </button>
        </div>
      </main>
    );
  } else if (route.name === 'login' || !me) {
    content = <Login onLogin={handleLogin} />;
  } else if (route.name === 'assistant' && me.role === 'teacher') {
    content = (
      <AssistantSettings
        me={me}
        theme={theme}
        onToggleTheme={toggleTheme}
        onLogout={handleLogout}
        onNavigate={navigate}
        onUnauthorized={onUnauthorized}
      />
    );
  } else if (route.name === 'knowledge') {
	content = (
		<Knowledge
			me={me}
			theme={theme}
			onToggleTheme={toggleTheme}
			onLogout={handleLogout}
			onNavigate={navigate}
			onUnauthorized={onUnauthorized}
		/>
	);
  } else if (route.name === 'conversations') {
    content = (
      <Conversations
        me={me}
        theme={theme}
        conversationId={route.conversationId}
        onToggleTheme={toggleTheme}
        onLogout={handleLogout}
        onNavigate={navigate}
        onOpenConversation={(id) => navigate(pathForRoute({ name: 'conversations', conversationId: id }))}
        onUnauthorized={onUnauthorized}
      />
    );
  } else {
    content = (
      <Materials
        me={me}
        theme={theme}
        view={view}
        onToggleTheme={toggleTheme}
        onToggleView={toggleView}
        onLogout={handleLogout}
        onNavigate={navigate}
        onUnauthorized={onUnauthorized}
        setPaletteCommands={setPaletteCommands}
      />
    );
  }

  return (
    <ToastProvider>
      {content}
      {paletteEl}
    </ToastProvider>
  );
}
