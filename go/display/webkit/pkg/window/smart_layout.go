package window

import (
	"context"
	"sort"

	core "dappco.re/go"
	"dappco.re/go/render/display/webkit/pkg/screen"
)

type schemeResponse struct {
	ContentType string
	Body        string
}

func (s *Service) buildWindowSpec(t TaskOpenWindow) (*Window, resultFailure) {
	if t.Window != nil {
		spec := *t.Window
		return &spec, nil
	}
	return ApplyOptions(t.Options...)
}

func (s *Service) prepareWindowSpec(spec *Window) resultFailure {
	if spec == nil {
		return core.E("window.prepareWindowSpec", "window spec is nil", nil)
	}

	rawURL := spec.URL
	preload := s.buildPreload(rawURL)
	if preload != "" {
		if spec.JS != "" {
			spec.JS = preload + "\n" + spec.JS
		} else {
			spec.JS = preload
		}
	}

	if !core.HasPrefix(rawURL, "core://") {
		return nil
	}

	resolved, ok, err := s.resolveCoreScheme(rawURL)
	if err != nil {
		return err
	}
	if !ok {
		return core.E("window.prepareWindowSpec", "core scheme handler unavailable for "+rawURL, nil)
	}
	spec.HTML = resolved.Body
	spec.URL = "about:blank"
	return nil
}

func (s *Service) buildPreload(rawURL string) string {
	if rawURL == "" {
		rawURL = "/"
	}
	result := s.Core().Action("display.buildPreload").Run(context.Background(), core.NewOptions(
		core.Option{Key: "url", Value: rawURL},
	))
	if !result.OK {
		return ""
	}
	script, _ := result.Value.(string)
	return script
}

func (s *Service) resolveCoreScheme(rawURL string) (schemeResponse, bool, resultFailure) {
	result := s.Core().Action("display.resolve_scheme").Run(context.Background(), core.NewOptions(
		core.Option{Key: "url", Value: rawURL},
	))
	if !result.OK {
		if err, ok := result.Value.(error); ok {
			return schemeResponse{}, false, err
		}
		return schemeResponse{}, false, nil
	}

	switch value := result.Value.(type) {
	case map[string]any:
		body, _ := value["body"].(string)
		contentType, _ := value["content_type"].(string)
		return schemeResponse{ContentType: contentType, Body: body}, true, nil
	default:
		return schemeResponse{}, false, nil
	}
}

func (s *Service) applyWindowBounds(name string, bounds WindowBounds) resultFailure {
	pw, ok := s.manager.Get(name)
	if !ok {
		return core.E("window.applyWindowBounds", "window not found: "+name, nil)
	}
	pw.SetBounds(bounds.X, bounds.Y, bounds.Width, bounds.Height)
	s.manager.State().UpdatePosition(name, bounds.X, bounds.Y)
	s.manager.State().UpdateSize(name, bounds.Width, bounds.Height)
	return nil
}

func (s *Service) defaultScreen() *screen.Screen {
	result := s.Core().QUERY(screen.QueryCurrent{})
	if result.OK {
		if value, ok := result.Value.(*screen.Screen); ok && value != nil {
			return value
		}
	}
	result = s.Core().QUERY(screen.QueryPrimary{})
	if result.OK {
		if value, ok := result.Value.(*screen.Screen); ok && value != nil {
			return value
		}
	}
	return nil
}

func (s *Service) screenByID(id string) *screen.Screen {
	if id == "" {
		return s.defaultScreen()
	}
	result := s.Core().QUERY(screen.QueryByID{ID: id})
	if !result.OK {
		return s.defaultScreen()
	}
	value, _ := result.Value.(*screen.Screen)
	if value == nil {
		return s.defaultScreen()
	}
	return value
}

func (s *Service) screenForWindow(name string) *screen.Screen {
	info := s.queryWindowByName(name)
	if info == nil {
		return s.defaultScreen()
	}
	result := s.Core().QUERY(screen.QueryAtPoint{
		X: info.X + max(info.Width/2, 1),
		Y: info.Y + max(info.Height/2, 1),
	})
	if !result.OK {
		return s.defaultScreen()
	}
	value, _ := result.Value.(*screen.Screen)
	if value == nil {
		return s.defaultScreen()
	}
	return value
}

func screenWorkArea(scr *screen.Screen) screen.Rect {
	if scr == nil {
		return screen.Rect{X: 0, Y: 0, Width: 1920, Height: 1080}
	}
	if scr.WorkArea.Width > 0 && scr.WorkArea.Height > 0 {
		return scr.WorkArea
	}
	if scr.Bounds.Width > 0 && scr.Bounds.Height > 0 {
		return scr.Bounds
	}
	return screen.Rect{X: 0, Y: 0, Width: 1920, Height: 1080}
}

func screenID(scr *screen.Screen) string {
	if scr == nil {
		return ""
	}
	return scr.ID
}

func normalizeSide(side string) string {
	switch core.Lower(core.Trim(side)) {
	case "left":
		return "left"
	case "right":
		return "right"
	default:
		return "auto"
	}
}

func preferredEditorWindow(windows []WindowInfo, target string, explicit string) *WindowInfo {
	if explicit != "" {
		for i := range windows {
			if windows[i].Name == explicit {
				return &windows[i]
			}
		}
	}

	editorHints := []string{"code", "cursor", "zed", "xcode", "idea", "goland", "webstorm", "clion", "fleet", "nvim", "vim"}
	for i := range windows {
		if windows[i].Name == target {
			continue
		}
		haystack := core.Lower(windows[i].Name + " " + windows[i].Title)
		for _, hint := range editorHints {
			if core.Contains(haystack, hint) {
				return &windows[i]
			}
		}
	}
	for i := range windows {
		if windows[i].Name == target {
			continue
		}
		if windows[i].Focused {
			return &windows[i]
		}
	}
	for i := range windows {
		if windows[i].Name != target {
			return &windows[i]
		}
	}
	return nil
}

func (s *Service) taskLayoutBesideEditor(task TaskLayoutBesideEditor) (LayoutBesideEditorResult, resultFailure) {
	target := s.queryWindowByName(task.Name)
	if target == nil {
		return LayoutBesideEditorResult{}, core.E("window.taskLayoutBesideEditor", "window not found: "+task.Name, nil)
	}

	editor := preferredEditorWindow(s.queryWindowList(), task.Name, task.Editor)
	if editor == nil {
		return LayoutBesideEditorResult{}, core.E("window.taskLayoutBesideEditor", "no editor window detected", nil)
	}

	scr := s.screenForWindow(editor.Name)
	area := screenWorkArea(scr)
	side := normalizeSide(task.Side)
	if side == "auto" {
		leftFree := editor.X - area.X
		rightFree := (area.X + area.Width) - (editor.X + editor.Width)
		if rightFree >= leftFree {
			side = "right"
		} else {
			side = "left"
		}
	}

	targetWidth := target.Width
	if targetWidth <= 0 {
		targetWidth = area.Width / 3
	}
	targetWidth = max(area.Width/4, min(targetWidth, area.Width/2))

	editorBounds := WindowBounds{X: editor.X, Y: editor.Y, Width: editor.Width, Height: editor.Height}
	windowBounds := WindowBounds{Width: targetWidth, Height: area.Height}

	leftFree := max(editor.X-area.X, 0)
	rightFree := max((area.X+area.Width)-(editor.X+editor.Width), 0)
	freeSpace := rightFree
	if side == "left" {
		freeSpace = leftFree
	}

	if freeSpace >= targetWidth {
		if side == "left" {
			windowBounds.X = area.X
		} else {
			windowBounds.X = area.X + area.Width - targetWidth
		}
		windowBounds.Y = area.Y
		windowBounds.Width = targetWidth
		windowBounds.Height = area.Height
	} else {
		ratio := task.Ratio
		if ratio <= 0 || ratio >= 1 {
			ratio = 0.62
		}
		editorWidth := int(float64(area.Width) * ratio)
		if editorWidth <= 0 || editorWidth >= area.Width {
			editorWidth = area.Width * 62 / 100
		}
		targetWidth = area.Width - editorWidth
		if side == "left" {
			windowBounds = WindowBounds{X: area.X, Y: area.Y, Width: targetWidth, Height: area.Height}
			editorBounds = WindowBounds{X: area.X + targetWidth, Y: area.Y, Width: editorWidth, Height: area.Height}
		} else {
			editorBounds = WindowBounds{X: area.X, Y: area.Y, Width: editorWidth, Height: area.Height}
			windowBounds = WindowBounds{X: area.X + editorWidth, Y: area.Y, Width: targetWidth, Height: area.Height}
		}
		if err := s.applyWindowBounds(editor.Name, editorBounds); err != nil {
			return LayoutBesideEditorResult{}, err
		}
	}

	if err := s.applyWindowBounds(task.Name, windowBounds); err != nil {
		return LayoutBesideEditorResult{}, err
	}

	return LayoutBesideEditorResult{
		Editor:       editor.Name,
		EditorBounds: editorBounds,
		WindowBounds: windowBounds,
		Side:         side,
		ScreenID:     screenID(scr),
	}, nil
}

func (s *Service) taskLayoutSuggest(task TaskLayoutSuggest) LayoutSuggestion {
	scr := s.screenByID(task.ScreenID)
	area := screenWorkArea(scr)
	windowCount := task.WindowCount
	if windowCount <= 0 {
		windowCount = len(s.manager.List())
	}

	mode := "presenting"
	reason := "single window fits the full work area"
	aspect := float64(area.Width) / float64(max(area.Height, 1))

	switch {
	case windowCount >= 4:
		mode = "grid"
		reason = "four or more windows benefit from equal cells"
	case windowCount == 3:
		if aspect >= 1.35 {
			mode = "coding"
			reason = "wide screen leaves room for a dominant editor plus side tools"
		} else {
			mode = "grid"
			reason = "balanced thirds fit better on a narrower screen"
		}
	case windowCount == 2:
		if aspect >= 1.2 {
			mode = "left-right"
			reason = "landscape screen favors a side-by-side split"
		} else {
			mode = "stack"
			reason = "portrait-like screen favors a vertical cascade"
		}
	}

	return LayoutSuggestion{
		Mode:     mode,
		Reason:   reason,
		ScreenID: screenID(scr),
		Width:    area.Width,
		Height:   area.Height,
	}
}

func expandRect(rect screen.Rect, padding int) screen.Rect {
	if padding <= 0 {
		return rect
	}
	return screen.Rect{
		X:      rect.X - padding,
		Y:      rect.Y - padding,
		Width:  rect.Width + padding*2,
		Height: rect.Height + padding*2,
	}
}

func rectContains(parent, child screen.Rect) bool {
	if child.IsEmpty() || parent.IsEmpty() {
		return false
	}
	return child.X >= parent.X &&
		child.Y >= parent.Y &&
		child.X+child.Width <= parent.X+parent.Width &&
		child.Y+child.Height <= parent.Y+parent.Height
}

func uniqueSorted(values []int) []int {
	sort.Ints(values)
	if len(values) == 0 {
		return values
	}
	out := values[:1]
	for _, value := range values[1:] {
		if value != out[len(out)-1] {
			out = append(out, value)
		}
	}
	return out
}

func intersectsAny(candidate screen.Rect, occupied []screen.Rect) bool {
	for _, rect := range occupied {
		if !candidate.Intersect(rect).IsEmpty() {
			return true
		}
	}
	return false
}

func (s *Service) taskScreenFindSpace(task TaskScreenFindSpace) ScreenSpace {
	scr := s.screenByID(task.ScreenID)
	area := screenWorkArea(scr)
	padding := max(task.Padding, 0)

	reqWidth := task.Width
	if reqWidth <= 0 {
		reqWidth = min(640, area.Width)
	}
	reqHeight := task.Height
	if reqHeight <= 0 {
		reqHeight = min(480, area.Height)
	}

	windows := s.queryWindowList()
	occupied := make([]screen.Rect, 0, len(windows))
	xEdges := []int{area.X, area.X + area.Width}
	yEdges := []int{area.Y, area.Y + area.Height}
	for _, win := range windows {
		rect := screen.Rect{X: win.X, Y: win.Y, Width: win.Width, Height: win.Height}
		rect = rect.Intersect(area)
		if rect.IsEmpty() {
			continue
		}
		rect = expandRect(rect, padding)
		occupied = append(occupied, rect)
		xEdges = append(xEdges, rect.X, rect.X+rect.Width)
		yEdges = append(yEdges, rect.Y, rect.Y+rect.Height)
	}
	xEdges = uniqueSorted(xEdges)
	yEdges = uniqueSorted(yEdges)

	best := ScreenSpace{
		ScreenID: screenID(scr),
		X:        area.X,
		Y:        area.Y,
		Width:    min(reqWidth, area.Width),
		Height:   min(reqHeight, area.Height),
	}
	bestArea := -1

	for i := 0; i < len(xEdges); i++ {
		for j := i + 1; j < len(xEdges); j++ {
			for k := 0; k < len(yEdges); k++ {
				for l := k + 1; l < len(yEdges); l++ {
					candidate := screen.Rect{
						X:      xEdges[i],
						Y:      yEdges[k],
						Width:  xEdges[j] - xEdges[i],
						Height: yEdges[l] - yEdges[k],
					}
					if candidate.Width < reqWidth || candidate.Height < reqHeight {
						continue
					}
					if !rectContains(area, candidate) || intersectsAny(candidate, occupied) {
						continue
					}
					areaScore := candidate.Width * candidate.Height
					if areaScore > bestArea {
						bestArea = areaScore
						best = ScreenSpace{
							ScreenID: screenID(scr),
							X:        candidate.X,
							Y:        candidate.Y,
							Width:    candidate.Width,
							Height:   candidate.Height,
						}
					}
				}
			}
		}
	}

	return best
}

func (s *Service) taskWindowArrangePair(task TaskWindowArrangePair) (PairArrangement, resultFailure) {
	if task.Primary == "" || task.Secondary == "" {
		return PairArrangement{}, core.E("window.taskWindowArrangePair", "primary and secondary windows are required", nil)
	}
	if _, ok := s.manager.Get(task.Primary); !ok {
		return PairArrangement{}, core.E("window.taskWindowArrangePair", "window not found: "+task.Primary, nil)
	}
	if _, ok := s.manager.Get(task.Secondary); !ok {
		return PairArrangement{}, core.E("window.taskWindowArrangePair", "window not found: "+task.Secondary, nil)
	}

	scr := s.screenByID(task.ScreenID)
	if task.ScreenID == "" {
		scr = s.screenForWindow(task.Primary)
	}
	area := screenWorkArea(scr)
	ratio := task.Ratio
	if ratio <= 0 || ratio >= 1 {
		ratio = 0.55
	}

	arrangement := PairArrangement{ScreenID: screenID(scr)}
	if area.Width >= area.Height {
		leftWidth := int(float64(area.Width) * ratio)
		arrangement.Orientation = "horizontal"
		arrangement.Primary = WindowBounds{X: area.X, Y: area.Y, Width: leftWidth, Height: area.Height}
		arrangement.Secondary = WindowBounds{X: area.X + leftWidth, Y: area.Y, Width: area.Width - leftWidth, Height: area.Height}
	} else {
		topHeight := int(float64(area.Height) * ratio)
		arrangement.Orientation = "vertical"
		arrangement.Primary = WindowBounds{X: area.X, Y: area.Y, Width: area.Width, Height: topHeight}
		arrangement.Secondary = WindowBounds{X: area.X, Y: area.Y + topHeight, Width: area.Width, Height: area.Height - topHeight}
	}

	if err := s.applyWindowBounds(task.Primary, arrangement.Primary); err != nil {
		return PairArrangement{}, err
	}
	if err := s.applyWindowBounds(task.Secondary, arrangement.Secondary); err != nil {
		return PairArrangement{}, err
	}

	return arrangement, nil
}
