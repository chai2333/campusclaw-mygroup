import { useEffect, useMemo, useRef, useState } from 'react';

export interface Command {
  id: string;
  label: string;
  hint?: string;
  keywords?: string;
  // skipFocusRestore: when the command focuses something itself (e.g. search
  // input), the palette must NOT restore focus to the previously focused
  // element after running.
  skipFocusRestore?: boolean;
  run: () => void;
}

interface Props {
  open: boolean;
  onClose: () => void;
  commands: Command[];
}

export function CommandPalette({ open, onClose, commands }: Props) {
  const [query, setQuery] = useState('');
  const [active, setActive] = useState(0);
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const prevFocus = useRef<HTMLElement | null>(null);
  const ranRef = useRef(false);

  // When opening, capture current focus, reset query, focus the input.
  useEffect(() => {
    if (open) {
      prevFocus.current = (document.activeElement as HTMLElement) || null;
      ranRef.current = false;
      setQuery('');
      setActive(0);
      // Defer focus to next frame so the input is mounted.
      const t = window.requestAnimationFrame(() => inputRef.current?.focus());
      return () => window.cancelAnimationFrame(t);
    }
    return undefined;
  }, [open]);

  // When closing, restore focus to the trigger unless a command took over.
  useEffect(() => {
    if (open) return undefined;
    if (prevFocus.current && !ranRef.current) {
      prevFocus.current.focus();
    }
    prevFocus.current = null;
    return undefined;
  }, [open]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return commands;
    return commands.filter((c) => {
      const haystack = `${c.label} ${c.keywords || ''} ${c.hint || ''}`.toLowerCase();
      return haystack.includes(q);
    });
  }, [query, commands]);

  // Reset selection whenever the filter changes.
  useEffect(() => {
    setActive(0);
  }, [filtered]);

  // Keyboard navigation while open.
  useEffect(() => {
    if (!open) return undefined;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        e.stopPropagation();
        onClose();
      } else if (e.key === 'ArrowDown') {
        e.preventDefault();
        setActive((a) => Math.min(a + 1, Math.max(filtered.length - 1, 0)));
      } else if (e.key === 'ArrowUp') {
        e.preventDefault();
        setActive((a) => Math.max(a - 1, 0));
      } else if (e.key === 'Enter') {
        e.preventDefault();
        const cmd = filtered[active];
        if (cmd) {
          ranRef.current = cmd.skipFocusRestore === true;
          cmd.run();
          onClose();
        }
      }
    };
    window.addEventListener('keydown', onKey, true);
    return () => window.removeEventListener('keydown', onKey, true);
  }, [open, filtered, active, onClose]);

  // Keep active item scrolled into view.
  useEffect(() => {
    if (!open) return;
    const list = listRef.current;
    if (!list) return;
    const el = list.children[active] as HTMLElement | undefined;
    el?.scrollIntoView({ block: 'nearest' });
  }, [active, open]);

  if (!open) return null;

  const run = (cmd: Command) => {
    ranRef.current = cmd.skipFocusRestore === true;
    cmd.run();
    onClose();
  };

  return (
    <div
      className="palette-overlay"
      role="dialog"
      aria-modal="true"
      aria-label="命令面板"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="palette" role="combobox" aria-expanded="true" aria-haspopup="listbox">
        <input
          ref={inputRef}
          className="palette-input"
          type="text"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="输入命令或搜索…"
          aria-label="搜索命令"
          aria-controls="palette-list"
          aria-autocomplete="list"
          autoComplete="off"
          spellCheck={false}
        />
        <div id="palette-list" className="palette-list" ref={listRef} role="listbox">
          {filtered.length === 0 && <div className="palette-empty">无匹配命令</div>}
          {filtered.map((cmd, i) => (
            <button
              key={cmd.id}
              type="button"
              className={`palette-item${i === active ? ' active' : ''}`}
              role="option"
              aria-selected={i === active}
              onMouseEnter={() => setActive(i)}
              onClick={() => run(cmd)}
            >
              <span className="palette-item-label">{cmd.label}</span>
              {cmd.hint && <span className="palette-item-hint">{cmd.hint}</span>}
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}
