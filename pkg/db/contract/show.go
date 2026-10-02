package contract

// Show is the data contract returned by the db layer for a row in the
// shows table.
type Show struct {
	ID           string
	Name         string
	PricePaise   int
	PerUserLimit int
}
