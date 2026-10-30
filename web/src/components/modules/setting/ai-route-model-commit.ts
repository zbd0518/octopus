export interface AIRouteModelCommit {
    select: (value: string) => void;
    change: () => void;
    blur: (value: string) => void;
}

export function createAIRouteModelCommit(save: (value: string) => void): AIRouteModelCommit {
    let selected: string | null = null;

    return {
        select(value) {
            selected = value;
            save(value);
        },
        change() {
            selected = null;
        },
        blur(value) {
            const isSelectionBlur = selected === value;
            selected = null;
            if (!isSelectionBlur) save(value);
        },
    };
}
