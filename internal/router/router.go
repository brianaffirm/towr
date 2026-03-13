package router

// Decision captures the routing choice and why it was made.
type Decision struct {
	Model           string
	Reason          string
	Tier            int
	CanEscalate     bool
	RequireApproval bool
}
