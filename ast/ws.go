package ast

// Python's \s / \S are Unicode-aware, Go RE2's are not — a single no-break space in a
// heading is enough for the two to split sections differently. This constant evens
// out the difference.
const ws = `[\p{Z}\t\n\f\r\v\x{1c}-\x{1f}\x{85}]`
