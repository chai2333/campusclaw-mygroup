const loginForm = document.querySelector('#login-form');
const logoutForm = document.querySelector('#logout-form');
const errorBox = document.querySelector('#auth-error');

async function submitAuth(event, url, body) {
  event.preventDefault();
  const button = event.currentTarget.querySelector('button');
  button.disabled = true;
  errorBox.textContent = '';
  try {
    const response = await fetch(url, {
      method: 'POST', credentials: 'same-origin',
      headers: {'Content-Type': 'application/json'}, body: JSON.stringify(body),
    });
    if (!response.ok) {
      errorBox.textContent = response.status === 401 ? '账号或密码错误。' : '操作失败，请重试。';
      return;
    }
    window.location.replace(url === '/api/login' ? '/materials' : '/login');
  } catch {
    errorBox.textContent = '无法连接服务器，请重试。';
  } finally {
    button.disabled = false;
  }
}

loginForm?.addEventListener('submit', event => {
  submitAuth(event, '/api/login', Object.fromEntries(new FormData(loginForm)));
});
logoutForm?.addEventListener('submit', event => submitAuth(event, '/api/logout', {}));

async function restoreIdentity() {
  try {
    const response = await fetch('/api/me', {credentials: 'same-origin', cache: 'no-store'});
    if (response.status === 401 && !loginForm) window.location.replace('/login');
    if (response.ok && loginForm) window.location.replace('/materials');
    if (!response.ok && response.status !== 401) errorBox.textContent = '无法确认登录状态，请刷新重试。';
  } catch {
    errorBox.textContent = '无法连接服务器，请刷新重试。';
  }
}
window.addEventListener('pageshow', restoreIdentity);
