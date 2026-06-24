import { useState, useRef, useEffect } from 'react';
import { createPortal } from 'react-dom';
import { MessageCircle, X, Send, Loader2, RotateCcw } from 'lucide-react';
import ReactMarkdown, { type Components } from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { Assistant, ApiError, type AssistantTurn } from '../lib/api';
import { useLanguage } from '../lib/language-context';

// 운영자 가이드 채팅 패널 — bluei-edge-assistant(26B)와 대화.
// 헤더 '도움말' 버튼 → 우측 슬라이드 패널. 어느 탭에서든 열고 작업 화면은 유지.
// 응답은 SSE 스트리밍(토큰 단위). 대화는 localStorage 에 자동 저장돼 패널을 닫거나
// 앱을 재시작해도 유지된다. 컨텍스트 자동 관리는 백엔드가 최근 N개로 제한.

const STORAGE_KEY = 'bluei-assistant-chat-v1';

const STARTER_KEYS = [
  'assistant.starter1',
  'assistant.starter2',
  'assistant.starter3',
  'assistant.starter4',
];

// 어시스턴트 답변(마크다운) 렌더링 — typography 플러그인 없이 요소별 스타일.
const md: Components = {
  p: ({ children }) => <p className="my-1 leading-relaxed">{children}</p>,
  ul: ({ children }) => <ul className="list-disc pl-5 my-1 space-y-0.5">{children}</ul>,
  ol: ({ children }) => <ol className="list-decimal pl-5 my-1 space-y-0.5">{children}</ol>,
  strong: ({ children }) => <strong className="font-semibold text-white">{children}</strong>,
  code: ({ children }) => <code className="bg-black/40 px-1 rounded text-green-300 text-xs">{children}</code>,
  h1: ({ children }) => <h3 className="font-bold text-white mt-2 mb-1">{children}</h3>,
  h2: ({ children }) => <h3 className="font-bold text-white mt-2 mb-1">{children}</h3>,
  h3: ({ children }) => <h3 className="font-bold text-white mt-2 mb-1">{children}</h3>,
  a: ({ children, href }) => <a href={href} className="text-green-400 underline">{children}</a>,
};

function loadMessages(): AssistantTurn[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (raw) return JSON.parse(raw) as AssistantTurn[];
  } catch { /* ignore */ }
  return [];
}

export function AssistantPanel() {
  const { tr } = useLanguage();
  const [open, setOpen] = useState(false);
  const [messages, setMessages] = useState<AssistantTurn[]>(loadMessages);
  const [input, setInput] = useState('');
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const scrollRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    scrollRef.current?.scrollTo({ top: scrollRef.current.scrollHeight, behavior: 'smooth' });
  }, [messages, loading]);

  // 자동 저장.
  useEffect(() => {
    try { localStorage.setItem(STORAGE_KEY, JSON.stringify(messages)); } catch { /* ignore */ }
  }, [messages]);

  const appendToLastAssistant = (delta: string) => {
    setMessages(prev => {
      const copy = [...prev];
      const li = copy.length - 1;
      copy[li] = { ...copy[li], content: copy[li].content + delta };
      return copy;
    });
  };

  const send = async (text: string) => {
    const q = text.trim();
    if (!q || loading) return;
    setErr(null);
    setInput('');
    const base: AssistantTurn[] = [...messages, { role: 'user', content: q }];
    // user 메시지 + 빈 assistant placeholder (스트리밍으로 채워짐).
    setMessages([...base, { role: 'assistant', content: '' }]);
    setLoading(true);
    try {
      await Assistant.chatStream(base, { onDelta: appendToLastAssistant });
    } catch (e) {
      const msg = e instanceof ApiError ? `${e.code}: ${e.message}` : String(e);
      setErr(msg);
      // 아무것도 못 받은 빈 assistant placeholder 는 제거.
      setMessages(prev => {
        const last = prev[prev.length - 1];
        if (last && last.role === 'assistant' && last.content === '') return prev.slice(0, -1);
        return prev;
      });
    } finally {
      setLoading(false);
    }
  };

  const newChat = () => {
    setMessages([]);
    setErr(null);
    try { localStorage.removeItem(STORAGE_KEY); } catch { /* ignore */ }
  };

  // 마지막 메시지가 빈 assistant 면 "작성 중" 상태.
  const lastMsg = messages[messages.length - 1];
  const awaitingFirstToken = loading && lastMsg?.role === 'assistant' && lastMsg.content === '';

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        title={tr('assistant.buttonTitle')}
        className="flex items-center gap-1.5 px-3 py-2 bg-green-500/10 border border-green-500/30 rounded-lg text-green-400 hover:bg-green-500/20 transition-colors"
      >
        <MessageCircle className="w-4 h-4" />
        <span className="text-sm font-medium">{tr('header.assistant')}</span>
      </button>

      {open && createPortal(
        <div className="fixed inset-0 z-[110] flex justify-end" onClick={() => setOpen(false)}>
          <div className="absolute inset-0 bg-black/40 backdrop-blur-[2px]" />
          <div
            className="relative w-full max-w-md h-full bg-gradient-to-b from-gray-900 to-black border-l border-green-500/30 flex flex-col shadow-2xl"
            onClick={e => e.stopPropagation()}
          >
            {/* 헤더 */}
            <div className="flex items-center justify-between px-4 py-3 border-b border-green-500/20">
              <div className="flex items-center gap-2 text-green-400">
                <MessageCircle className="w-5 h-5" />
                <h2 className="text-base font-bold text-white">{tr('assistant.title')}</h2>
              </div>
              <div className="flex items-center gap-1">
                {messages.length > 0 && (
                  <button
                    type="button"
                    onClick={newChat}
                    className="flex items-center gap-1 px-2 py-1.5 rounded-lg text-gray-400 hover:bg-gray-800 hover:text-white text-xs"
                    title={tr('assistant.newChatTitle')}
                  >
                    <RotateCcw className="w-3.5 h-3.5" />
                    {tr('assistant.newChat')}
                  </button>
                )}
                <button
                  type="button"
                  onClick={() => setOpen(false)}
                  className="p-1.5 rounded-lg text-gray-400 hover:bg-gray-800 hover:text-white"
                  title={tr('assistant.close')}
                >
                  <X className="w-5 h-5" />
                </button>
              </div>
            </div>

            {/* 메시지 영역 */}
            <div ref={scrollRef} className="flex-1 overflow-y-auto px-4 py-4 space-y-3">
              {messages.length === 0 && (
                <div className="space-y-3">
                  <p className="text-sm text-gray-400 leading-relaxed">
                    bluei-edge {tr('assistant.emptyIntro')}<br />
                    <span className="text-gray-500">{tr('assistant.emptyDisclaimer')}</span>
                  </p>
                  <div className="flex flex-col gap-2">
                    {STARTER_KEYS.map(k => {
                      const text = tr(k);
                      return (
                        <button
                          key={k}
                          type="button"
                          onClick={() => void send(text)}
                          className="text-left text-sm px-3 py-2 rounded-lg border border-gray-700 text-gray-300 hover:bg-gray-800 hover:border-green-500/40 transition-colors"
                        >
                          {text}
                        </button>
                      );
                    })}
                  </div>
                </div>
              )}

              {messages.map((m, i) => (
                <div key={i} className={m.role === 'user' ? 'flex justify-end' : 'flex justify-start'}>
                  {m.role === 'user' ? (
                    <div className="max-w-[85%] rounded-2xl rounded-br-sm bg-green-600/90 text-white px-3 py-2 text-sm whitespace-pre-wrap">
                      {m.content}
                    </div>
                  ) : (
                    <div className="max-w-[90%] rounded-2xl rounded-bl-sm bg-gray-800 text-gray-100 px-3 py-2 text-sm">
                      {m.content
                        ? <ReactMarkdown remarkPlugins={[remarkGfm]} components={md}>{m.content}</ReactMarkdown>
                        : awaitingFirstToken && (
                            <span className="text-gray-400 flex items-center gap-2">
                              <Loader2 className="w-4 h-4 animate-spin" />{tr('assistant.writing')}
                            </span>
                          )}
                    </div>
                  )}
                </div>
              ))}

              {err && (
                <div className="text-sm text-red-400 bg-red-500/10 border border-red-500/30 rounded-lg px-3 py-2">
                  {tr('assistant.error', { error: err })}
                </div>
              )}
            </div>

            {/* 입력 영역 */}
            <div className="border-t border-green-500/20 p-3">
              <form
                onSubmit={e => { e.preventDefault(); void send(input); }}
                className="flex items-end gap-2"
              >
                <textarea
                  value={input}
                  onChange={e => setInput(e.target.value)}
                  onKeyDown={e => {
                    if (e.key === 'Enter' && !e.shiftKey) {
                      e.preventDefault();
                      void send(input);
                    }
                  }}
                  rows={1}
                  placeholder={tr('assistant.placeholder')}
                  className="flex-1 resize-none rounded-lg bg-gray-800 border border-gray-700 px-3 py-2 text-sm text-white placeholder-gray-500 focus:outline-none focus:border-green-500/50 max-h-32"
                />
                <button
                  type="submit"
                  disabled={loading || !input.trim()}
                  className="p-2 rounded-lg bg-green-600 text-white hover:bg-green-500 disabled:opacity-40 disabled:cursor-not-allowed"
                  title={tr('assistant.send')}
                >
                  <Send className="w-4 h-4" />
                </button>
              </form>
            </div>
          </div>
        </div>,
        document.body,
      )}
    </>
  );
}
