package auth

type RegisterRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type UserInfo struct {
	ID       uint   `json:"id"`
	Username string `json:"username"`
	Nickname string `json:"nickname,omitempty"`
	Email    string `json:"email,omitempty"`
}

type LoginResponse struct {
	Token            string   `json:"token"`
	ExpiresInSeconds int      `json:"expires_in_seconds"`
	User             UserInfo `json:"user"`
}
