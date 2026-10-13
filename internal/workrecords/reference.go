package workrecords

// Reference carries only list-visible metadata, never form values or notes.
type Reference struct {
	ID     uint64 `json:"id,string"`
	Type   string `json:"type"`
	Number string `json:"number"`
	Title  string `json:"title"`
	State  string `json:"state"`
	URL    string `json:"url"`
	Exact  bool   `json:"exact"`
}
