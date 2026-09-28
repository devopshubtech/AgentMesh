export class ArgvParseError extends Error {}

/**
 * Splits a single command line into argv, shell-like (POSIX-ish):
 * - whitespace separates arguments
 * - '...' preserves everything literally
 * - "..." preserves whitespace; backslash escapes \" \\ \$ \` inside
 * - outside quotes a backslash escapes the next character
 * - adjacent quoted/unquoted pieces are concatenated (a"b c"d -> "ab cd")
 * - "" / '' produce an empty argument
 * No variable expansion, globbing or operators are interpreted.
 */
export function splitArgv(input: string): string[] {
  const args: string[] = [];
  let current = '';
  let inArg = false;
  let quote: '"' | "'" | null = null;

  for (let i = 0; i < input.length; i++) {
    const ch = input.charAt(i);
    if (quote === "'") {
      if (ch === "'") quote = null;
      else current += ch;
      continue;
    }
    if (quote === '"') {
      const next = input.charAt(i + 1);
      if (ch === '"') {
        quote = null;
      } else if (ch === '\\' && next !== '' && '"\\$`'.includes(next)) {
        current += next;
        i++;
      } else {
        current += ch;
      }
      continue;
    }
    if (ch === "'" || ch === '"') {
      quote = ch;
      inArg = true;
      continue;
    }
    if (ch === '\\') {
      if (i + 1 < input.length) {
        current += input.charAt(i + 1);
        i++;
      } else {
        current += ch;
      }
      inArg = true;
      continue;
    }
    if (/\s/.test(ch)) {
      if (inArg) {
        args.push(current);
        current = '';
        inArg = false;
      }
      continue;
    }
    current += ch;
    inArg = true;
  }
  if (quote) {
    throw new ArgvParseError(`Unterminated ${quote === '"' ? 'double' : 'single'} quote`);
  }
  if (inArg) args.push(current);
  return args;
}

/** Renders argv back to a readable single line (quotes args containing special characters). */
export function joinArgv(argv: readonly string[]): string {
  return argv
    .map((a) => {
      if (a === '') return "''";
      if (/^[A-Za-z0-9_@%+=:,./\\-]+$/.test(a)) return a;
      return "'" + a.split("'").join("'\\''") + "'";
    })
    .join(' ');
}
