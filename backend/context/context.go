package context

import (
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/auth0/go-jwt-middleware"
	"github.com/codegangsta/negroni"
	"github.com/dgrijalva/jwt-go"
	"github.com/gorilla/mux"
	"github.com/nicksnyder/go-i18n/i18n"
)

var (
	verifyKey []byte
	signKey   []byte
	endpoints = []*Endpoint{}
)

type ContextHandler func(c *Context, rw http.ResponseWriter, req *http.Request) error

type MethodHandlers map[string]ContextHandler

func (handlers *MethodHandlers) IsAllowed(req *http.Request) bool {
	for method, _ := range *handlers {
		if req.Method == method {
			return true
		}
	}
	return false
}

type Endpoint struct {
	Public   bool
	Path     string
	Handlers MethodHandlers
}

func AddEndpoint(endpoint *Endpoint) {
	endpoints = append(endpoints, endpoint)
}

func Configure(router *mux.Router, privKey, pubKey, pathPrefix string, vars map[string]interface{}) error {
	apiRouter := router.PathPrefix(pathPrefix).Subrouter()

	LoadSecureKeys(privKey, pubKey)
	ctx, err := New(apiRouter, vars)
	if err != nil {
		return err
	}
	ctx.AddEndpoints(endpoints...)

	return nil
}

type Context struct {
	Router *mux.Router
	Vars   map[string]interface{}

	T     i18n.TranslateFunc
	Token *jwt.Token

	middleware *jwtmiddleware.JWTMiddleware
}

func New(router *mux.Router, vars map[string]interface{}) (c *Context, err error) {
	c = &Context{
		Router: router,
		Vars:   vars,
	}
	options := jwtmiddleware.Options{
		ValidationKeyGetter: func(token *jwt.Token) (interface{}, error) {
			return verifyKey, nil
		},
	}
	c.middleware = jwtmiddleware.New(options)
	return c, nil
}

func (c *Context) AddEndpoint(endpoint *Endpoint) {
	httpHandler := newContextHandler(c, endpoint)
	if endpoint.Public {
		c.Router.HandleFunc(endpoint.Path, httpHandler)
	} else {
		c.Router.Handle(
			endpoint.Path, negroni.New(
				negroni.HandlerFunc(c.middleware.HandlerWithNext),
				negroni.Wrap(http.HandlerFunc(httpHandler)),
			),
		)
	}
}

func (c *Context) AddEndpoints(endpoints ...*Endpoint) {
	for _, endpoint := range endpoints {
		c.AddEndpoint(endpoint)
	}
}

func SignToken(token *jwt.Token) (string, error) {
	return token.SignedString(signKey)
}

func LoadSecureKeys(privateKeyPath, publicKeyPath string) (err error) {
	signKey, err = ioutil.ReadFile(privateKeyPath)
	if err != nil {
		return fmt.Errorf("Error reading private key")
	}
	verifyKey, err = ioutil.ReadFile(publicKeyPath)
	if err != nil {
		return fmt.Errorf("Error reading public key")
	}
	return nil
}

func newContextHandler(context *Context, endpoint *Endpoint) http.HandlerFunc {
	return func(rw http.ResponseWriter, req *http.Request) {
		if !endpoint.Handlers.IsAllowed(req) {
			http.Error(rw, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !endpoint.Public {
			context.Token, _ = jwt.ParseFromRequest(
				req, context.middleware.Options.ValidationKeyGetter,
			)
		}
		context.updateT(req)
		err := endpoint.Handlers[req.Method](context, rw, req)
		if err != nil {
			http.Error(rw, err.Error(), http.StatusBadRequest)
		}
	}
}

func (c *Context) updateT(req *http.Request) {
	acceptLang := req.Header.Get("Accept-Language")
	defaultLang := "en-US"
	c.T = i18n.MustTfunc(acceptLang, defaultLang)
}
