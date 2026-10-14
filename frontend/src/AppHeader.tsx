import type { ReactNode } from 'react';
import type { Me } from './api';

export type Theme = 'light' | 'dark';
export type PageName = 'materials' | 'knowledge' | 'conversations' | 'assistant';

interface Props {
  me: Me;
  theme: Theme;
  active: PageName;
  onToggleTheme: () => void;
  onLogout: () => void;
  onNavigate: (path: string) => void;
  // Extra header controls (e.g. upload, view toggle) rendered before the
  // theme/logout cluster.
  actions?: ReactNode;
}

// Shared application header with role-scoped navigation. Students never see
// the assistant-settings entry; teachers never see the conversation entry.
export function AppHeader({ me, theme, active, onToggleTheme, onLogout, onNavigate, actions }: Props) {
  const navItems: Array<{ page: PageName; label: string; path: string }> = [
    { page: 'materials', label: '资料库', path: '/materials' },
  ];
  {
    navItems.push({ page: 'conversations', label: 'AI 助手', path: '/conversations' });
  }
  if (me.role === 'teacher') {
    navItems.push({ page: 'assistant', label: '智能体与技能', path: '/assistant' });
  }
  return (
    <header className="app-header">
      <div className="app-brand">
        <span className="brand-mark" aria-hidden="true">C</span>
        <span className="brand-name">CampusClaw</span>
      </div>
      <nav className="app-nav" aria-label="页面导航">
        {navItems.map((item) => (
          <button
            key={item.page}
            type="button"
            className={`nav-link${active === item.page ? ' active' : ''}`}
            aria-current={active === item.page ? 'page' : undefined}
            onClick={() => onNavigate(item.path)}
          >
            {item.label}
          </button>
        ))}
      </nav>
      <span className="header-user" title={me.username}>
        <span>{me.username}</span>
        <span className="role-tag">{me.role === 'teacher' ? '教师' : '学生'}</span>
      </span>
      <div className="header-actions">
        {actions}
        <button
          type="button"
          className="btn btn-sm"
          onClick={onToggleTheme}
          aria-label={theme === 'light' ? '切换到深色主题' : '切换到浅色主题'}
          title={theme === 'light' ? '深色主题' : '浅色主题'}
        >
          {theme === 'light' ? '🌙' : '☀'}
        </button>
        <button type="button" className="btn btn-sm" onClick={onLogout}>
          退出
        </button>
      </div>
    </header>
  );
}
