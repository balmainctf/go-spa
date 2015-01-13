package account

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"regexp"

	log "github.com/Sirupsen/logrus"
	"github.com/gotk/ctx"
	"github.com/gotk/pg"

	"github.com/rafael84/go-spa/backend/base"
	"github.com/rafael84/go-spa/backend/mail"
)

type ValidKey struct {
	UserId int64  `json:"userId"`
	Key    string `json:"key"`
}

func init() {
	ctx.Resource("/account/reset-password", &ResetPasswordResource{}, true)
	ctx.Resource("/account/reset-password/validate-key", &ValidateKeyResource{}, true)
	ctx.Resource("/account/reset-password/complete", &CompleteResource{}, true)
}

func sendResetPasswordEmail(c *ctx.Context, user *User) {
	var body bytes.Buffer

	resetTokenService := NewResetTokenService(c.Vars["db"].(*pg.Session))

	resetToken, err := resetTokenService.Create(user.Id.NullInt64.Int64)
	if err != nil {
		log.Errorf("Unable to create a new reset token: %s", err)
		return
	}

	body.WriteString("Access this link: ")
	body.WriteString("http://localhost:3000/#/reset-password/step2/")
	body.WriteString(resetToken.Key)

	err = mail.NewGmailAccount(
		os.Getenv("EMAIL_USERNAME"),
		os.Getenv("EMAIL_PASSWORD"),
	).Send(&mail.Message{
		From:    "Go-SPA",
		To:      []string{user.Email},
		Subject: "Reset Password",
		Body:    body.Bytes(),
	})

	if err != nil {
		log.Errorf("Unable to send email: %s", err)
		return
	}

}

type ResetPasswordResource struct {
	*base.Resource
}

func (r *ResetPasswordResource) POST(c *ctx.Context, rw http.ResponseWriter, req *http.Request) error {
	// decode request data
	var form ResetPasswordForm
	err := json.NewDecoder(req.Body).Decode(&form)
	if err != nil {
		return ctx.BadRequest(rw, "Could not query user: %s", err)
	}

	// validate email address
	if ok := regexp.MustCompile(emailRegex).MatchString(form.Email); !ok {
		return ctx.BadRequest(rw, "Invalid email address")
	}

	// create new user service
	userService := NewUserService(r.DB(c))

	// get user from database
	var user *User
	user, err = userService.GetByEmail(form.Email)
	if err != nil {
		return ctx.BadRequest(rw, "User not found")
	}

	go sendResetPasswordEmail(c, user)

	return ctx.OK(rw, "Email sent")
}

type ValidateKeyResource struct {
	*base.Resource
}

func (r *ValidateKeyResource) POST(c *ctx.Context, rw http.ResponseWriter, req *http.Request) error {
	type ValidateKeyForm struct {
		Key string `json:"key"`
	}

	// decode request data
	var form ValidateKeyForm
	err := json.NewDecoder(req.Body).Decode(&form)
	if err != nil {
		return ctx.BadRequest(rw, "Unable to validate key")
	}

	service := NewResetTokenService(r.DB(c))

	resetToken, err := service.GetByKey(form.Key)
	if err != nil || !resetToken.Valid() {
		return ctx.BadRequest(rw, "Invalid Key")
	}

	return ctx.OK(rw, ValidKey{resetToken.UserId, form.Key})
}

type CompleteResource struct {
	*base.Resource
}

func (r *CompleteResource) POST(c *ctx.Context, rw http.ResponseWriter, req *http.Request) error {
	type ChangePasswordForm struct {
		Password      string   `json:"password"`
		PasswordAgain string   `json:"passwordAgain"`
		ValidKey      ValidKey `json:"validKey"`
	}

	// decode request data
	var form ChangePasswordForm
	err := json.NewDecoder(req.Body).Decode(&form)
	if err != nil {
		return ctx.BadRequest(rw, "Unable to change the password")
	}

	// validate the passwords
	if form.Password != form.PasswordAgain {
		return ctx.BadRequest(rw, "Passwords mismatch")
	}

	// validate the key again
	resetTokenService := NewResetTokenService(r.DB(c))
	resetToken, err := resetTokenService.GetByKey(form.ValidKey.Key)
	if err != nil || !resetToken.Valid() {
		return ctx.BadRequest(rw, "Invalid Key")
	}

	// get user from db
	userService := NewUserService(r.DB(c))
	user, err := userService.GetById(resetToken.UserId)
	if err != nil {
		return ctx.InternalServerError(rw, "User not found")
	}

	// encode user password
	err = user.Password.Encode(form.Password)
	if err != nil {
		return ctx.InternalServerError(rw, "Could not change user password")
	}

	// change user data in database
	err = userService.Update(user)
	if err != nil {
		return ctx.InternalServerError(rw, "Could not change user password")
	}

	// invalidate token
	resetToken.State = ResetTokenInactive
	err = resetTokenService.Update(resetToken)
	if err != nil {
		log.Errorf("Unable to invalidate token: %s", err)
	}

	return ctx.OK(rw, user)
}
