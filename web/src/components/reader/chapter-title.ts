/**
 * AO3 chapter headings read "Chapter 3" or "Chapter 3: The Title". The reader
 * numbers chapters itself, so only a real title survives as the subtitle; a
 * heading in any other shape ("Prologue") is kept whole.
 */
export function chapterHeading(
  title: string | undefined,
  index: number,
): { label: string; subtitle?: string } {
  const label = `第 ${index + 1} 章`;
  const raw = title?.trim() ?? "";
  const match = /^chapter\s+\d+\s*(?:[:：.\-–—]\s*(.*))?$/i.exec(raw);
  const subtitle = (match ? match[1] : raw)?.trim();
  return { label, subtitle: subtitle || undefined };
}
