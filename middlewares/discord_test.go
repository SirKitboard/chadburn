package middlewares

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"

	. "gopkg.in/check.v1"
)

type SuiteDiscord struct {
	BaseSuite
}

var _ = Suite(&SuiteDiscord{})

func (s *SuiteDiscord) TestNewDiscordEmpty(c *C) {
	c.Assert(NewDiscord(&DiscordConfig{}), IsNil)
}

func (s *SuiteDiscord) TestRunSuccess(c *C) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var p discordWebhookPayload
		json.Unmarshal(body, &p)

		c.Assert(len(p.Embeds), Equals, 1)
		c.Assert(p.Embeds[0].Color, Equals, discordColorSuccess)
	}))
	defer ts.Close()

	s.ctx.Start()
	s.ctx.Stop(nil)

	m := NewDiscord(&DiscordConfig{DiscordWebhook: ts.URL})
	c.Assert(m.Run(s.ctx), IsNil)
}

func (s *SuiteDiscord) TestRunFailed(c *C) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var p discordWebhookPayload
		json.Unmarshal(body, &p)

		c.Assert(len(p.Embeds), Equals, 1)
		c.Assert(p.Embeds[0].Color, Equals, discordColorFailure)
	}))
	defer ts.Close()

	s.ctx.Start()
	s.ctx.Stop(errors.New("something went wrong"))

	m := NewDiscord(&DiscordConfig{DiscordWebhook: ts.URL})
	c.Assert(m.Run(s.ctx), NotNil)
}

func (s *SuiteDiscord) TestOnlyOnErrorSuppressesSuccess(c *C) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c.Assert(true, Equals, false) // should never be called
	}))
	defer ts.Close()

	s.ctx.Start()
	s.ctx.Stop(nil)

	m := NewDiscord(&DiscordConfig{DiscordWebhook: ts.URL, DiscordOnlyOnError: true})
	c.Assert(m.Run(s.ctx), IsNil)
}
