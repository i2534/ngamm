package mgr

import "strings"

// https://github.com/Cp0204/quark-auto-save/blob/main/quark_auto_save.py
type QuarkPan struct {
}

// implement the interface Pan
func (p *QuarkPan) Name() string {
	return "quarkpan"
}
func (p *QuarkPan) Init() error {
	return nil
}
func (p *QuarkPan) Support(pmd PanMetadata) bool {
	return strings.Contains(pmd.URL, "pan.quark.cn")
}
func (p *QuarkPan) Transfer(topicId int, pmd PanMetadata) error {
	return nil
}
