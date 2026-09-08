package loom

// 注册表合并：产物是一张「事件 → 处理器」的表，节点是**一条注册项**，
// 身份由配置里的正则从条目里提取（通常是被调用的脚本名）。
//
// 经线那条原样进、纬线按身份取代同名的那条、纬线独有的追加。
// 【为什么不能朴素并集】同一个处理器会被注册两次 —— 而重复注册的注册表往往直接失效，
// 且失效得很安静：文件仍是合法 JSON，只是行为翻倍。

import (
	"fmt"

	"github.com/axfor/loom/ast"
)

func mergeRegistry(c *Config, t *Template) (string, error) {
	if c.RegGroup == "" || c.RegID == nil {
		return "", fmt.Errorf("%s: type = \"json\" 需要 loom.lm 里有一个 registry 块"+
			"（group + id_pattern）—— 不知道拿什么当身份就没法去重", t.Path)
	}
	from := t.From
	if from == "" {
		from = c.Warp
	}
	upSrc, _, err := c.read(from, t.BasePath)
	if err != nil {
		return "", err
	}
	wfSrc, _, err := c.read(c.Weft, t.Target)
	if err != nil {
		return "", err
	}
	up, err := ast.Parse(upSrc)
	if err != nil {
		return "", fmt.Errorf("%s：%v", t.BasePath, err)
	}
	wf, err := ast.Parse(wfSrc)
	if err != nil {
		return "", fmt.Errorf("%s：%v", t.Target, err)
	}

	// 纬线已经注册了的 (事件, 身份)
	taken := map[[2]string]bool{}
	forEachEntry(c, wf, func(ev, id string, _ *ast.Value) {
		taken[[2]string{ev, id}] = true
	})

	merged := ast.NewObject()
	group := ast.NewObject()
	merged.Set(c.RegGroup, group)
	kept := 0

	if ug, ok := up.Get(c.RegGroup); ok && ug.Kind == ast.Object {
		for _, ev := range ug.Keys {
			for _, g := range ug.Props[ev].Elems {
				hs, ok := g.Get("hooks")
				if !ok || hs.Kind != ast.Array {
					continue
				}
				kf := ast.NewArray()
				for _, h := range hs.Elems {
					if taken[[2]string{ev, entryID(c, h)}] {
						continue
					}
					kf.Elems = append(kf.Elems, h)
				}
				if len(kf.Elems) == 0 {
					continue
				}
				ng := g.Clone()
				ng.Set("hooks", kf)
				appendTo(group, ev, ng)
				kept += len(kf.Elems)
			}
		}
	}
	if wg, ok := wf.Get(c.RegGroup); ok && wg.Kind == ast.Object {
		for _, ev := range wg.Keys {
			for _, g := range wg.Props[ev].Elems {
				appendTo(group, ev, g)
			}
		}
	}

	// 【为什么要数】合并出错时产物仍是合法 JSON，只是少了几条或多了几条 ——
	// 而少注册一个门与门没生效是同一件事，且两者都不会报错。
	if got, want := countEntries(merged, c), countEntries(wf, c)+kept; got != want {
		return "", fmt.Errorf("%s: 合并后条数对不上（纬线 %d + 经线保留 %d ≠ %d）",
			t.Path, countEntries(wf, c), kept, got)
	}
	return merged.Marshal() + "\n", nil
}

func appendTo(group *ast.Value, ev string, g *ast.Value) {
	arr, ok := group.Get(ev)
	if !ok {
		arr = ast.NewArray()
		group.Set(ev, arr)
	}
	arr.Elems = append(arr.Elems, g)
}

func entryID(c *Config, h *ast.Value) string {
	cmd, ok := h.Get("command")
	if !ok || cmd.Kind != ast.String {
		return ""
	}
	m := c.RegID.FindStringSubmatch(cmd.Str)
	if m == nil || len(m) < 2 {
		return ""
	}
	return m[1]
}

func forEachEntry(c *Config, root *ast.Value, fn func(ev, id string, h *ast.Value)) {
	g, ok := root.Get(c.RegGroup)
	if !ok || g.Kind != ast.Object {
		return
	}
	for _, ev := range g.Keys {
		for _, grp := range g.Props[ev].Elems {
			hs, ok := grp.Get("hooks")
			if !ok || hs.Kind != ast.Array {
				continue
			}
			for _, h := range hs.Elems {
				if id := entryID(c, h); id != "" {
					fn(ev, id, h)
				}
			}
		}
	}
}

func countEntries(root *ast.Value, c *Config) int {
	n := 0
	g, ok := root.Get(c.RegGroup)
	if !ok || g.Kind != ast.Object {
		return 0
	}
	for _, ev := range g.Keys {
		for _, grp := range g.Props[ev].Elems {
			if hs, ok := grp.Get("hooks"); ok && hs.Kind == ast.Array {
				n += len(hs.Elems)
			}
		}
	}
	return n
}
