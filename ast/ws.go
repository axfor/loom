package ast

// Python 的 \s / \S 是 Unicode 感知的，Go RE2 的不是 —— 标题里一个不换行空格
// 就能让两边切出不同的节。这两个常量把差异抹平。
const ws = `[\p{Z}\t\n\f\r\v\x{1c}-\x{1f}\x{85}]`
