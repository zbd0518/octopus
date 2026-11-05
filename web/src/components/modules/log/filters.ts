export interface NamedGroupItem {
    id: number;
    name: string;
}

/**
 * 按照分组名称升序排序，名称相同时按 ID 升序决胜
 */
export function sortGroupsByName<T extends NamedGroupItem>(groups: readonly T[]): T[] {
    return [...groups].sort((a, b) => a.name.localeCompare(b.name) || a.id - b.id);
}
