'use client';

import { Component, type ErrorInfo, type ReactNode } from 'react';
import { reportError } from '@/lib/error-report';

interface Props {
    children: ReactNode;
}

interface State {
    hasError: boolean;
    message: string;
}

/**
 * 应用级错误边界：捕获子树渲染/生命周期错误，上报后端错误日志，
 * 并展示可恢复的 fallback（重载按钮），避免白屏后无法操作。
 */
export class AppErrorBoundary extends Component<Props, State> {
    state: State = { hasError: false, message: '' };

    static getDerivedStateFromError(error: unknown): State {
        return {
            hasError: true,
            message: error instanceof Error ? error.message : String(error),
        };
    }

    componentDidCatch(error: unknown, errorInfo: ErrorInfo) {
        void reportError({
            level: 'error',
            message: error instanceof Error ? error.message : String(error),
            stack: [error instanceof Error ? error.stack : '', errorInfo.componentStack].filter(Boolean).join('\n'),
        });
    }

    private handleReload = () => {
        window.location.reload();
    };

    render() {
        if (!this.state.hasError) {
            return this.props.children;
        }
        return (
            <div className="flex min-h-[60vh] flex-col items-center justify-center gap-4 rounded-xl border border-border/35 bg-card p-8 text-center">
                <p className="text-lg font-semibold text-foreground">Something went wrong</p>
                <p className="max-w-md text-sm break-words text-muted-foreground">
                    {this.state.message || 'An unexpected error occurred while rendering this page.'}
                </p>
                <button
                    type="button"
                    onClick={this.handleReload}
                    className="rounded-lg border border-border/50 bg-background px-4 py-2 text-sm text-foreground transition-colors hover:bg-muted"
                >
                    Reload
                </button>
            </div>
        );
    }
}
