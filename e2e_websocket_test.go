package httpexpect

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	fastwebsocket "github.com/fasthttp-contrib/websocket"
	"github.com/gorilla/websocket"
	"github.com/valyala/fasthttp"
)

type wsHandlerOpts struct {
	preRead  func()
	preWrite func()
}

func createWebsocketHandler(opts wsHandlerOpts) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/test", func(w http.ResponseWriter, r *http.Request) {
		upgrader := &websocket.Upgrader{}
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			panic(err)
		}
		defer c.Close()
		for {
			if opts.preRead != nil {
				opts.preRead()
			}
			mt, message, err := c.ReadMessage()
			if err != nil {
				break
			}
			if opts.preWrite != nil {
				opts.preWrite()
			}
			err = c.WriteMessage(mt, message)
			if err != nil {
				break
			}
		}
	})

	return mux
}

func websocketFastHandler(ctx *fasthttp.RequestCtx) {
	upgrader := fastwebsocket.New(func(c *fastwebsocket.Conn) {
		defer c.Close()
		for {
			mt, message, err := c.ReadMessage()
			if err != nil {
				break
			}
			err = c.WriteMessage(mt, message)
			if err != nil {
				break
			}
		}
	})
	err := upgrader.Upgrade(ctx)
	if err != nil {
		panic(err)
	}
}

func testWebsocketSession(e *Expect) {
	ws := e.GET("/test").WithWebsocketUpgrade().
		Expect().
		Status(http.StatusSwitchingProtocols).
		Websocket()
	defer ws.Disconnect()

	ws.WriteBytesBinary([]byte("my binary bytes")).
		Expect().
		BinaryMessage().Body().Equal("my binary bytes")

	ws.WriteBytesText([]byte("my text bytes")).
		Expect().
		TextMessage().Body().Equal("my text bytes")

	ws.WriteText("my text").
		Expect().
		TextMessage().Body().Equal("my text")

	ws.WriteJSON(struct {
		Message string `json:"message"`
	}{"my json"}).
		Expect().
		TextMessage().JSON().Object().ValueEqual("message", "my json")

	ws.CloseWithText("my close message").
		Expect().
		CloseMessage().NoContent()
}

func testWebsocketTypes(e *Expect) {
	ws := e.GET("/test").WithWebsocketUpgrade().
		Expect().
		Status(http.StatusSwitchingProtocols).
		Websocket()
	defer ws.Disconnect()

	ws.WriteMessage(websocket.TextMessage, []byte("test")).
		Expect().
		Type(websocket.TextMessage).Body().Equal("test")

	ws.WriteMessage(websocket.BinaryMessage, []byte("test")).
		Expect().
		Type(websocket.BinaryMessage).Body().Equal("test")

	ws.WriteMessage(websocket.CloseMessage, []byte("test")).
		Expect().
		Type(websocket.CloseMessage).NoContent()
}

func testWebsocket(e *Expect) {
	testWebsocketSession(e)
	testWebsocketTypes(e)
}

func TestE2EWebsocketLive(t *testing.T) {
	handler := createWebsocketHandler(wsHandlerOpts{})

	server := httptest.NewServer(handler)
	defer server.Close()

	e := WithConfig(Config{
		BaseURL:  server.URL,
		Reporter: NewAssertReporter(t),
	})

	testWebsocket(e)
}

func TestE2EWebsocketHandlerStandard(t *testing.T) {
	handler := createWebsocketHandler(wsHandlerOpts{})

	e := WithConfig(Config{
		Reporter:        NewAssertReporter(t),
		WebsocketDialer: NewWebsocketDialer(handler),
	})

	testWebsocket(e)
}

func TestE2EWebsocketHandlerFast(t *testing.T) {
	e := WithConfig(Config{
		Reporter:        NewAssertReporter(t),
		WebsocketDialer: NewFastWebsocketDialer(websocketFastHandler),
	})

	testWebsocket(e)
}

func testWebsocketTimeout(
	t *testing.T,
	handler http.Handler,
	blockCh chan struct{},
	timeout bool,
	setupFn func(*Websocket),
) {
	server := httptest.NewServer(handler)
	defer server.Close()

	e := WithConfig(Config{
		BaseURL:  server.URL,
		Reporter: newMockReporter(t),
	})

	ws := e.GET("/test").WithWebsocketUpgrade().
		Expect().
		Status(http.StatusSwitchingProtocols).
		Websocket()
	defer ws.Disconnect()

	setupFn(ws)

	blockCh <- struct{}{}

	ws.WriteText("test").Expect()
	ws.chain.assertOK(t)

	go func() {
		time.Sleep(time.Millisecond * 100)
		blockCh <- struct{}{}
	}()

	ws.WriteText("test").Expect()
	if timeout {
		ws.chain.assertFailed(t)
	} else {
		ws.chain.assertOK(t)
	}
}

func TestE2EWebsocketTimeouts(t *testing.T) {
	t.Run("with-read-timeout", func(t *testing.T) {
		blockCh := make(chan struct{}, 1)

		handler := createWebsocketHandler(wsHandlerOpts{
			preWrite: func() {
				<-blockCh
			},
		})

		testWebsocketTimeout(t, handler, blockCh, true, func(ws *Websocket) {
			ws.WithReadTimeout(time.Millisecond * 10)
		})
	})

	t.Run("without-read-timeout", func(t *testing.T) {
		blockCh := make(chan struct{}, 1)

		handler := createWebsocketHandler(wsHandlerOpts{
			preWrite: func() {
				<-blockCh
			},
		})

		testWebsocketTimeout(t, handler, blockCh, false, func(ws *Websocket) {
			ws.WithoutReadTimeout()
		})
	})

	t.Run("without-write-timeout", func(t *testing.T) {
		blockCh := make(chan struct{}, 1)

		handler := createWebsocketHandler(wsHandlerOpts{
			preRead: func() {
				<-blockCh
			},
		})

		testWebsocketTimeout(t, handler, blockCh, false, func(ws *Websocket) {
			ws.WithoutWriteTimeout()
		})
	})
}
