import { describe, expect, it } from 'vitest';
import { ArgvParseError, joinArgv, splitArgv } from './argv';

describe('splitArgv', () => {
  it('splits on whitespace', () => {
    expect(splitArgv('uname -a')).toEqual(['uname', '-a']);
    expect(splitArgv('  ls   -la\t/tmp  ')).toEqual(['ls', '-la', '/tmp']);
    expect(splitArgv('')).toEqual([]);
    expect(splitArgv('   ')).toEqual([]);
  });
  it('respects double quotes', () => {
    expect(splitArgv('echo "hello world"')).toEqual(['echo', 'hello world']);
    expect(splitArgv('echo "say \\"hi\\""')).toEqual(['echo', 'say "hi"']);
    expect(splitArgv('echo "a\\nb"')).toEqual(['echo', 'a\\nb']);
  });
  it('respects single quotes literally', () => {
    expect(splitArgv("grep -r 'foo bar' .")).toEqual(['grep', '-r', 'foo bar', '.']);
    expect(splitArgv("echo 'a \\\" b'")).toEqual(['echo', 'a \\" b']);
  });
  it('concatenates adjacent pieces and supports empty args', () => {
    expect(splitArgv('a"b c"d')).toEqual(['ab cd']);
    expect(splitArgv('cmd "" x')).toEqual(['cmd', '', 'x']);
    expect(splitArgv("cmd ''")).toEqual(['cmd', '']);
  });
  it('handles backslash escapes outside quotes', () => {
    expect(splitArgv('echo a\\ b')).toEqual(['echo', 'a b']);
    expect(splitArgv('C:\\\\Windows')).toEqual(['C:\\Windows']);
  });
  it('does not interpret shell operators', () => {
    expect(splitArgv('ls | wc -l; rm -rf $HOME')).toEqual([
      'ls',
      '|',
      'wc',
      '-l;',
      'rm',
      '-rf',
      '$HOME',
    ]);
  });
  it('throws on unterminated quotes', () => {
    expect(() => splitArgv('echo "oops')).toThrow(ArgvParseError);
    expect(() => splitArgv("echo 'oops")).toThrow(ArgvParseError);
  });
});

describe('joinArgv', () => {
  it('round-trips', () => {
    const cases = [['uname', '-a'], ['echo', 'hello world'], ['x', ''], ['say', "it's"]];
    for (const argv of cases) {
      expect(splitArgv(joinArgv(argv))).toEqual(argv);
    }
  });
});
