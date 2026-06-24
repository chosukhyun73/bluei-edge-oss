import { Component, type ErrorInfo, type ReactNode } from 'react';
import { tr as trBase, isSupportedLanguage, DEFAULT_LANGUAGE } from '../lib/i18n';

// 클래스 컴포넌트는 useLanguage 훅을 쓸 수 없어 localStorage 에서 직접 언어를 읽어 tr 호출.
function t(key: string): string {
  let lang = DEFAULT_LANGUAGE;
  try {
    const v = localStorage.getItem('bluei_edge_lang');
    if (isSupportedLanguage(v)) lang = v;
  } catch {
    /* localStorage 접근 불가 — 기본 언어 사용 */
  }
  return trBase(key, lang);
}

interface Props {
  children: ReactNode;
  fallbackTitle?: string;
}

interface State {
  hasError: boolean;
  error: Error | null;
}

// 365일 무중단 정책: 한 컴포넌트의 렌더 throw 가 dashboard 전체를
// 검은 화면으로 만들면 안 됨. Error Boundary 가 격리 + 복구 UI 제공.
export class ErrorBoundary extends Component<Props, State> {
  state: State = { hasError: false, error: null };

  static getDerivedStateFromError(error: Error): State {
    return { hasError: true, error };
  }

  componentDidCatch(error: Error, info: ErrorInfo) {
    console.error('[ErrorBoundary]', error, info.componentStack);
  }

  private reset = () => this.setState({ hasError: false, error: null });

  render() {
    if (this.state.hasError) {
      return (
        <div className="m-4 px-5 py-4 bg-destructive/10 border border-destructive/30 rounded-lg">
          <h2 className="text-lg font-bold text-destructive mb-2">
            {this.props.fallbackTitle ?? t('errorBoundary.defaultTitle')}
          </h2>
          <p className="text-sm text-gray-400 font-mono mb-3">
            {this.state.error?.message ?? t('errorBoundary.unknownError')}
          </p>
          <button
            onClick={this.reset}
            className="text-xs text-green-400 hover:text-green-300 font-mono py-1 px-3 border border-green-500/30 hover:border-green-500/50 rounded transition-colors"
          >
            {t('errorBoundary.retry')}
          </button>
        </div>
      );
    }
    return this.props.children;
  }
}
