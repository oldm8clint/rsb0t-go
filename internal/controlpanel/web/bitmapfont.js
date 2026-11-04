// Native cache glyph outlines and exact PixFont advances, prepared offline by
// tools/capture/build-panel-font.py. DOM text stays selectable and accessible;
// unsupported characters use the normal browser fallback font.
export function setBitmapText(node, text, font = 'p12', maxLen = 40) {
    node.textContent = text || '';
    node.classList.add('native-font-label');
    node.style.fontFamily = font === 'p11' ? "'Lost City Small', Arial, sans-serif" : "'Lost City', Arial, sans-serif";
    node.style.fontWeight = font === 'b12' ? 'bold' : 'normal';
}
