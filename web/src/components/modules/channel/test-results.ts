export type TestOutcome = 'success' | 'partialSuccess' | 'failed';

export function channelTestOutcome(summary: { passed: boolean; results: { passed: boolean }[] }): TestOutcome {
    const passed = summary.results.filter((result) => result.passed).length;
    if (passed === 0) return 'failed';
    return passed === summary.results.length ? 'success' : 'partialSuccess';
}
