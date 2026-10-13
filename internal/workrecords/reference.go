package workrecords

// Reference carries only list-visible metadata, never form values or notes.
type Reference struct {
	Class   string `json:"class,omitempty"`
	Version uint64 `json:"version,string"`
	ID      uint64 `json:"id,string"`
	Type    string `json:"type"`
	Number  string `json:"number"`
	Title   string `json:"title"`
	State   string `json:"state"`
	URL     string `json:"url"`
	Exact   bool   `json:"exact"`
}
