import { useState, type FormEvent } from 'react';
import { ApiError } from './api';

interface Props {
  onLogin: (username: string, password: string) => Promise<void>;
}

const friendlyErrors: Record<string, string> = {
  invalid_credentials: '用户名或密码错误',
  invalid_login_request: '请输入用户名和密码',
};

function describe(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 0) return '网络错误，无法连接服务';
    return friendlyErrors[err.code] || err.code || '登录失败';
  }
  return '网络错误，请稍后重试';
}

export function Login({ onLogin }: Props) {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault();
    if (submitting) return;
    setError(null);
    setSubmitting(true);
    try {
      await onLogin(username, password);
      // Parent navigates on success; this component will unmount.
    } catch (err) {
      setError(describe(err));
      setSubmitting(false);
    }
  };

  return (
    <main className="login-page">
      <div className="login-card">
        <div className="login-brand">
          <div className="login-logo" aria-hidden="true">C</div>
          <h1 className="login-title">CampusClaw</h1>
          <p className="login-subtitle">校园资料中心</p>
        </div>
        <form className="login-form" onSubmit={onSubmit} noValidate>
          <label className="field">
            <span className="field-label">用户名</span>
            <input
              className="field-input"
              type="text"
              name="username"
              autoComplete="username"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              required
              disabled={submitting}
              aria-label="用户名"
            />
          </label>
          <label className="field">
            <span className="field-label">密码</span>
            <input
              className="field-input"
              type="password"
              name="password"
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
              disabled={submitting}
              aria-label="密码"
            />
          </label>
          {error && (
            <div className="login-error" role="alert">
              {error}
            </div>
          )}
          <button
            className="btn btn-primary btn-block"
            type="submit"
            disabled={submitting}
          >
            {submitting ? '登录中…' : '登录'}
          </button>
        </form>
      </div>
    </main>
  );
}
