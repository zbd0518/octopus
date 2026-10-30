type ClipboardLike = Pick<Clipboard, 'writeText'>;
type TextAreaLike = Pick<HTMLTextAreaElement, 'value' | 'setAttribute' | 'select'> & {
    style: Pick<CSSStyleDeclaration, 'position' | 'top' | 'left'>;
};

type DocumentLike<T extends TextAreaLike> = {
    createElement: (tag: 'textarea') => T;
    execCommand: (command: string) => boolean;
    body: {
        appendChild: (node: T) => void;
        removeChild: (node: T) => void;
    };
};

type ClipboardOptions<T extends TextAreaLike> = {
    clipboard?: ClipboardLike;
    document?: DocumentLike<T>;
};

function fallbackCopyText<T extends TextAreaLike>(text: string, documentLike?: DocumentLike<T>) {
    if (!documentLike) {
        throw new Error('Clipboard unavailable');
    }

    const textArea = documentLike.createElement('textarea');
    textArea.value = text;
    textArea.setAttribute('readonly', 'true');
    textArea.style.position = 'fixed';
    textArea.style.top = '0';
    textArea.style.left = '-9999px';

    documentLike.body.appendChild(textArea);
    textArea.select();

    try {
        if (!documentLike.execCommand('copy')) {
            throw new Error('Clipboard unavailable');
        }
    } finally {
        documentLike.body.removeChild(textArea);
    }
}

export async function writeClipboardText<T extends TextAreaLike = HTMLTextAreaElement>(text: string, options: ClipboardOptions<T> = {}) {
    const clipboardLike = options.clipboard ?? navigator.clipboard;

    try {
        await clipboardLike.writeText(text);
    } catch (error) {
        if (options.document) {
            fallbackCopyText(text, options.document);
            return;
        }
        if (typeof document !== 'undefined') {
            fallbackCopyText<HTMLTextAreaElement>(text, document);
            return;
        }
        throw error;
    }
}
