package contract

type Show struct {
	ID           string
	Name         string
	PricePaise   int
	PerUserLimit int
}

type ShowSeatAvailability struct {
	ShowID    string
	Available int64
}
