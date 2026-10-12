package incidents

// Shared object policy for asynchronous consumers and late notification reads.
func CanRead(a Actor, r Record) bool { return readable(a, r) }
func CanReadNotes(a Actor) bool      { return notesAllowed(a) }
