layer "upstream" {
  dir  = "upstream"
  role = "warp"
}

layer "mine" {
  dir  = "mine"
  role = "weft"

  mark "markdown" {
    begin = "<!-- MINE:BEGIN -->"
    end   = "<!-- MINE:END -->"
  }
}

templates = "templates"

registry {
  group      = "hooks"
  id_pattern = "hooks/([A-Za-z0-9._-]+\\.(?:sh|js|py))"
}
