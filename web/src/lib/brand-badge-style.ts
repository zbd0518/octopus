/** sRGB relative luminance in [0, 1]; used for contrast-safe badge text. */
function brandColorLuminance(hex: string): number {
    const raw = hex.trim().replace('#', '');
    if (raw.length !== 6 && raw.length !== 3) return 0.5;
    const full = raw.length === 3
        ? raw.split('').map((c) => c + c).join('')
        : raw;
    const r = parseInt(full.slice(0, 2), 16) / 255;
    const g = parseInt(full.slice(2, 4), 16) / 255;
    const b = parseInt(full.slice(4, 6), 16) / 255;
    return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

/**
 * Badge style from a model brand color that stays readable in light/dark themes.
 * Very dark brands (Grok/Kimi #000) get light text in dark mode; very light brands
 * (Ollama #FFF) get dark text in light mode. Background keeps a brand-tinted wash.
 */
export function brandBadgeStyle(brandColor: string, isDark: boolean): { backgroundColor: string; color: string } {
    const lum = brandColorLuminance(brandColor);
    let color = brandColor;
    if (isDark && lum < 0.35) {
        color = '#E8E8E8';
    } else if (!isDark && lum > 0.8) {
        color = '#333333';
    }
    return {
        // Slightly stronger wash in dark mode so near-black brands still show a tint
        backgroundColor: `${brandColor}${isDark ? '33' : '15'}`,
        color,
    };
}
