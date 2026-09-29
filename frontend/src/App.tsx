import { useCallback, useEffect, useState, type ReactNode } from 'react';
import {
  ApiError,
  fetchMe,
  login as apiLogin,
  logout as apiLogout,
  type Me,
} from './api';
import { CommandPalette, type Command } from './CommandPalette';
import { Login } from './Login';
import { Materials } from './Materials';
import { ToastProvider } from './Toast';

type Route = 'loading' | 'login' | 'materials' | 'unavailable';
type Theme = 'light' | 'dark';
type ViewPref = 'list' | 'grid';

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

// Update the URL without dispatching a synthetic popstate. Route state is set
// directly by callers; the popstate listener only reacts to real back/forward
// navigation (after state has flushed).
function pushPath(path: string) {
  if (path !== window.location.pathname) {
    history.pushState({}, '', path);
  }
}

export function App() {
  const [route, setRoute] = useState<Route>('loading');
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

  // Identity check on mount: 200 -> route, 401 -> /login (no session clear),
  // 503 -> service unavailable WITHOUT clearing session.
  const checkIdentity = useCallback(async () => {
    setRoute('loading');
    try {
      const identity = await fetchMe();
      setMe(identity);
      const path = currentPath();
      if (path === '/login') {
        pushPath('/materials');
      }
      setRoute('materials');
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.status === 401) {
          setMe(null);
          setPaletteCommands([]);
          const path = currentPath();
          if (path !== '/login') {
            pushPath('/login');
          }
          setRoute('login');
        } else if (err.status === 503 || err.status === 0) {
          // Service unavailable or unreachable: do NOT clear session.
          setRoute('unavailable');
        } else {
          // Treat other unexpected statuses like unavailable without logout.
          setRoute('unavailable');
        }
      } else {
        setRoute('unavailable');
      }
    }
  }, []);

  useEffect(() => {
    checkIdentity();
  }, [checkIdentity]);

  // Respond to back/forward: keep route in sync with the URL.
  useEffect(() => {
    const onPop = () => {
      const path = currentPath();
      if (!me) return;
      if (path === '/login') {
        setRoute('login');
      } else if (path === '/materials' || path === '/') {
        setRoute('materials');
      }
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

  const handleLogin = useCallback(async (username: string, password: string) => {
    await apiLogin(username, password);
    const identity = await fetchMe();
    setMe(identity);
    pushPath('/materials');
    setRoute('materials');
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
    setRoute('login');
  }, []);

  const onUnauthorized = useCallback(() => {
    // 401 on a business request: clear in-memory state, redirect to /login.
    setMe(null);
    setPaletteCommands([]);
    pushPath('/login');
    setRoute('login');
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
    if (route === 'login') {
      setPaletteCommands([
        {
          id: 'theme',
          label: theme === 'light' ? '切换到深色主题' : '切换到浅色主题',
          keywords: 'theme toggle dark light',
          run: toggleTheme,
        },
      ]);
    }
    // Materials registers its own commands via setPaletteCommands.
  }, [route, theme, toggleTheme]);

  const paletteEl = (
    <CommandPalette
      open={paletteOpen}
      onClose={() => setPaletteOpen(false)}
      commands={paletteCommands}
    />
  );

  let content: ReactNode;
  if (route === 'loading') {
    content = (
      <main className="status-page" role="status" aria-live="polite">
        <div className="status-card">
          <span className="spinner" />
          <p>正在加载…</p>
        </div>
      </main>
    );
  } else if (route === 'unavailable') {
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
  } else if (route === 'login' || !me) {
    content = <Login onLogin={handleLogin} />;
  } else {
    content = (
      <Materials
        me={me}
        theme={theme}
        view={view}
        onToggleTheme={toggleTheme}
        onToggleView={toggleView}
        onLogout={handleLogout}
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
