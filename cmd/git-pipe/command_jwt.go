package main

import (
	"fmt"
	"time"

	"github.com/dgrijalva/jwt-go"
	"github.com/reddec/git-pipe/router"
)

type CommandJWT struct {
	Secret     string        `long:"secret" short:"s" env:"SECRET" description:"Shared JWT secret" required:"yes"`
	Group      string        `long:"group" short:"g" env:"GROUP" description:"Allowed group (repo name)"`
	Expiration time.Duration `long:"expiration" short:"e" env:"EXPIRATION" description:"Expiration time"`
	Methods    []string      `long:"methods" short:"m" env:"METHODS" description:"Allowed HTTP methods"`
	Args       struct {
		Name []string `positional-arg-name:"name" required:"1" description:"Client names for each token will be generated"`
	} `positional-args:"true"`
}

func (cmd *CommandJWT) Execute([]string) error {
	now := time.Now()
	for _, name := range cmd.Args.Name {
		var claims router.JWTClaims
		claims.Audience = name
		claims.Subject = cmd.Group
		claims.Methods = cmd.Methods
		claims.IssuedAt = now.Unix()
		if cmd.Expiration > 0 {
			claims.ExpiresAt = now.Add(cmd.Expiration).Unix()
		}

		token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, &claims).SignedString([]byte(cmd.Secret))
		if err != nil {
			return fmt.Errorf("sign string: %w", err)
		}
		fmt.Println(token) // nolint:forbidigo
	}
	return nil
}
