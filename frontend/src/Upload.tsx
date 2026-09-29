import { useEffect, useRef, useState, type FormEvent } from 'react';
import { ApiError, uploadMaterial } from './api';

interface Props {
  onClose: () => void;
  onUploaded: () => void;
  onUnauthorized: () => void;
  onToast: (message: string, kind: 'success' | 'error' | 'info') => void;
}

function describeError(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 403) return '没有上传权限';
    if (err.status === 413) return '文件过大，请压缩后重试';
    if (err.status === 400) return '上传请求无效';
    if (err.status === 0) return '网络错误，上传失败';
    return err.code || '上传失败';
  }
  return '网络错误，请稍后重试';
}

export function Upload({ onClose, onUploaded, onUnauthorized, onToast }: Props) {
  const [file, setFile] = useState<File | null>(null);
  const [progress, setProgress] = useState(0);
  const [uploading, setUploading] = useState(false);
  const [ingesting, setIngesting] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const abortRef = useRef<AbortController | null>(null);

  // Close on Escape when idle.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !uploading && !ingesting) {
        onClose();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [uploading, ingesting, onClose]);

  // Autofocus the file input on mount.
  useEffect(() => {
    inputRef.current?.focus();
  }, []);

  const onSubmit = async (e: FormEvent) => {
    e.preventDefault();
    if (!file || uploading || ingesting) return;
    setUploading(true);
    setProgress(0);
    setIngesting(false);
    const controller = new AbortController();
    abortRef.current = controller;
    try {
      await uploadMaterial({
        file,
        signal: controller.signal,
        onProgress: (p) => {
          setProgress(p.percent);
          // Once bytes are fully sent, show the "ingesting" state until 201.
          if (p.percent >= 100) {
            setUploading(false);
            setIngesting(true);
          }
        },
      });
      setIngesting(false);
      onToast('上传成功', 'success');
      onUploaded();
      onClose();
    } catch (err) {
      setIngesting(false);
      setUploading(false);
      if (err instanceof ApiError && err.status === 401) {
        onUnauthorized();
        return;
      }
      const msg = (err as Error)?.message;
      if (msg === 'aborted') {
        onToast('已取消上传', 'info');
        return;
      }
      onToast(describeError(err), 'error');
    } finally {
      abortRef.current = null;
    }
  };

  const onAbort = () => {
    abortRef.current?.abort();
  };

  const busy = uploading || ingesting;

  return (
    <div
      className="modal-overlay"
      role="dialog"
      aria-modal="true"
      aria-label="上传资料"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget && !busy) onClose();
      }}
    >
      <div className="modal">
        <div className="modal-header">
          <h2 className="modal-title">上传资料</h2>
          <button
            type="button"
            className="icon-btn"
            aria-label="关闭"
            onClick={() => !busy && onClose()}
            disabled={busy}
          >
            ×
          </button>
        </div>
        <form className="modal-body" onSubmit={onSubmit}>
          <label className="field">
            <span className="field-label">选择文件</span>
            <input
              ref={inputRef}
              type="file"
              className="file-input"
              onChange={(e) => setFile(e.target.files?.[0] || null)}
              disabled={busy}
              aria-label="选择文件"
            />
          </label>
          {file && (
            <div className="upload-file-info">
              <span className="upload-file-name" title={file.name}>
                {file.name}
              </span>
              <span className="upload-file-size">
                {formatSize(file.size)}
              </span>
            </div>
          )}
          {(uploading || ingesting) && (
            <div className="upload-progress">
              <div
                className="progress-track"
                role="progressbar"
                aria-valuenow={progress}
                aria-valuemin={0}
                aria-valuemax={100}
              >
                <div className="progress-bar" style={{ width: `${progress}%` }} />
              </div>
              <div className="progress-label" aria-live="polite">
                {ingesting ? '服务端处理中…' : `上传中 ${progress}%`}
              </div>
            </div>
          )}
          <div className="modal-actions">
            {uploading ? (
              <button
                type="button"
                className="btn btn-danger"
                onClick={onAbort}
              >
                取消上传
              </button>
            ) : (
              <>
                <button
                  type="button"
                  className="btn"
                  onClick={onClose}
                  disabled={busy}
                >
                  取消
                </button>
                <button
                  type="submit"
                  className="btn btn-primary"
                  disabled={!file || busy}
                >
                  {ingesting ? '处理中…' : '上传'}
                </button>
              </>
            )}
          </div>
        </form>
      </div>
    </div>
  );
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
  return `${(bytes / 1024 / 1024 / 1024).toFixed(1)} GB`;
}
