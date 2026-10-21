package auth

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"food-agent/pkg/model"
	"food-agent/pkg/token"
	"food-agent/pkg/utils/pwdutil"

	"gorm.io/gorm"
)

var (
	ErrNotFound = errors.New("not found")

	ErrInvalidUsername = errors.New("invalid username")
	ErrPasswordTooWeak = errors.New("password too weak")
	ErrDuplicateName   = errors.New("duplicate name")
	ErrUnauthorized    = errors.New("unauthorized")
	ErrUserNotFound    = errors.New("user not found")
	ErrInvalidUID      = errors.New("invalid uid")
)

// UserDAO isolates persistence operations for auth.
//
// Production uses pkg/dao.UserDAO; tests can provide an in-memory implementation.
type UserDAO interface {
	Create(ctx context.Context, user *model.User) error
	GetByUsername(ctx context.Context, username string) (*model.User, error)
	GetByID(ctx context.Context, id uint) (*model.User, error)
}

type Service struct {
	users             UserDAO
	tokens            token.Manager
	jwtExpiresSeconds int
}

func NewService(d Deps) (*Service, error) {
	users, tokens, exp, err := validateDeps(d)
	if err != nil {
		return nil, err
	}
	return &Service{
		users:             users,
		tokens:            tokens,
		jwtExpiresSeconds: exp,
	}, nil
}

func (s *Service) Register(ctx context.Context, username string, password string) (UserInfo, error) {
	username = strings.TrimSpace(username)
	if !usernameRe.MatchString(username) {
		return UserInfo{}, ErrInvalidUsername
	}
	if !pwdutil.CheckPasswordRating(password, pwdutil.Moderate) {
		return UserInfo{}, ErrPasswordTooWeak
	}

	if u, err := s.users.GetByUsername(ctx, username); err == nil && u != nil {
		return UserInfo{}, ErrDuplicateName
	} else if err != nil && !errors.Is(err, ErrNotFound) && !errors.Is(err, gorm.ErrRecordNotFound) {
		return UserInfo{}, err
	}

	hash, err := pwdutil.PasswordHash(password)
	if err != nil {
		return UserInfo{}, err
	}
	user := &model.User{
		Username:     username,
		PasswordHash: hash,
	}
	if err := s.users.Create(ctx, user); err != nil {
		return UserInfo{}, err
	}
	return UserInfo{ID: user.ID, Username: user.Username}, nil
}

func (s *Service) Login(ctx context.Context, username string, password string) (LoginResponse, error) {
	username = strings.TrimSpace(username)
	if !usernameRe.MatchString(username) || strings.TrimSpace(password) == "" {
		return LoginResponse{}, ErrUnauthorized
	}

	u, err := s.users.GetByUsername(ctx, username)
	if err != nil {
		// Avoid user enumeration.
		return LoginResponse{}, ErrUnauthorized
	}
	if !pwdutil.PasswordVerify(password, u.PasswordHash) {
		return LoginResponse{}, ErrUnauthorized
	}

	expires := time.Duration(s.jwtExpiresSeconds) * time.Second
	tok, err := s.tokens.IssueTo(token.Info{
		UID:      strconv.FormatUint(uint64(u.ID), 10),
		Username: u.Username,
		Name:     u.Nickname,
	}, expires)
	if err != nil {
		return LoginResponse{}, err
	}

	return LoginResponse{
		Token:            tok,
		ExpiresInSeconds: s.jwtExpiresSeconds,
		User: UserInfo{
			ID:       u.ID,
			Username: u.Username,
			Nickname: u.Nickname,
			Email:    u.Email,
		},
	}, nil
}

func (s *Service) Me(ctx context.Context, uid string) (UserInfo, error) {
	id64, err := strconv.ParseUint(strings.TrimSpace(uid), 10, 64)
	if err != nil || id64 == 0 {
		return UserInfo{}, ErrInvalidUID
	}
	u, err := s.users.GetByID(ctx, uint(id64))
	if err != nil {
		if errors.Is(err, ErrNotFound) || errors.Is(err, gorm.ErrRecordNotFound) {
			return UserInfo{}, ErrUserNotFound
		}
		return UserInfo{}, err
	}
	return UserInfo{
		ID:       u.ID,
		Username: u.Username,
		Nickname: u.Nickname,
		Email:    u.Email,
	}, nil
}
