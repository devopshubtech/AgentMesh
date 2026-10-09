import { cn } from '@/lib/cn';

/** Decorative QR-like pattern for illustrations (not a real, scannable code). */
export function QrArt({ className }: { className?: string }) {
  const cells: [number, number][] = [];
  let seed = 7;
  for (let y = 0; y < 21; y++) {
    for (let x = 0; x < 21; x++) {
      const finder = (x < 7 && y < 7) || (x > 13 && y < 7) || (x < 7 && y > 13);
      seed = (seed * 9301 + 49297) % 233280;
      if (!finder && seed / 233280 > 0.55) cells.push([x, y]);
    }
  }
  const finder = (ox: number, oy: number) => (
    <g key={`${ox}-${oy}`}>
      <rect x={ox} y={oy} width={7} height={7} className="fill-neutral-900" />
      <rect x={ox + 1} y={oy + 1} width={5} height={5} className="fill-white" />
      <rect x={ox + 2} y={oy + 2} width={3} height={3} className="fill-neutral-900" />
    </g>
  );
  return (
    <svg viewBox="-1 -1 23 23" className={cn('rounded-md bg-white p-1', className)} role="img" aria-label="QR code">
      {finder(0, 0)}
      {finder(14, 0)}
      {finder(0, 14)}
      {cells.map(([x, y]) => (
        <rect key={`${x}.${y}`} x={x} y={y} width={1} height={1} className="fill-neutral-900" />
      ))}
    </svg>
  );
}
