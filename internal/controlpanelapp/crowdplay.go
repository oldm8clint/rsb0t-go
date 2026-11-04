package controlpanelapp

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/controlpanel"
	"rsb0t-go/internal/crowdplay"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/pathfind"
)

// CrowdPlay via Discord (internal/crowdplay), OFF by default: nothing starts unless a token
// file is given. The token is read from that file at startup, by path, and never printed.
// The crowd only ever drives the "crowdplay" account; see docs/crowdplay_discord.md.
var (
	crowdTokenFile = flag.String("crowdplay-discord-token-file", "", "OPTIONAL, off by default: file holding the CrowdPlay Discord bot token (read by path at startup, never printed). With -crowdplay-discord-channel, a Discord bot reads !walk/!fish/!interact/!wait/!stop from that one channel and drives ONLY the \"crowdplay\" account; bank, trade, drop and give-away commands are always refused")
	crowdChannel   = flag.String("crowdplay-discord-channel", "", "the numeric id of the ONE Discord channel CrowdPlay reads commands from and replies in")
	crowdAPI       = flag.String("crowdplay-discord-api", crowdplay.DefaultAPIBase, "Discord REST base (the lab points this at a local fake; leave the default otherwise)")
	crowdMods      = flag.String("crowdplay-moderators", "", "comma-separated Discord user ids whose commands (such as !stop) skip the rate limits")
)

// crowdGame adapts the panel's FleetSource to crowdplay.Game.
type crowdGame struct{ src *controlpanel.FleetSource }

func (g crowdGame) Snapshot(account string) (gamestate.RenderSnapshot, error) {
	return g.src.RenderSnapshot(account)
}
func (g crowdGame) Run(account string, fn func(actions.Session) error) error {
	if account != crowdplay.Account {
		return fmt.Errorf("refusing account %q", account)
	}
	return g.src.WithStreamSession(account, fn)
}
func (g crowdGame) BotRunning(account string) string {
	for _, a := range g.src.Accounts(nil) {
		if a.Name == account && (a.BotState == "running" || a.BotState == "stopping") {
			return a.Bot
		}
	}
	return ""
}
func (g crowdGame) Pack() *pathfind.Pack    { return g.src.StreamCollisionPack() }
func (g crowdGame) Names() *cachecfg.Config { return g.src.StreamNames() }

// startCrowdPlay starts the Discord bot and the crowd runner when configured. A bad
// configuration is logged and CrowdPlay stays off; the panel itself keeps running.
func startCrowdPlay(ctx context.Context, src *controlpanel.FleetSource) {
	if *crowdTokenFile == "" {
		return
	}
	fail := func(err error) { log.Printf("controlpanel: CrowdPlay NOT started: %v", err) }
	token, err := crowdplay.ReadTokenFile(*crowdTokenFile)
	if err != nil {
		fail(err)
		return
	}
	found := false
	for _, a := range src.Accounts(nil) {
		found = found || a.Name == crowdplay.Account
	}
	if !found {
		fail(fmt.Errorf("the %q account is not in the roster", crowdplay.Account))
		return
	}
	d, err := crowdplay.NewDiscord(crowdplay.DiscordConfig{Token: token, ChannelID: *crowdChannel, APIBase: *crowdAPI,
		Log: func(line string) { log.Printf("controlpanel: %s", line) }})
	if err != nil {
		fail(err)
		return
	}
	mods := map[string]bool{}
	for _, id := range strings.Split(*crowdMods, ",") {
		if id = strings.TrimSpace(id); id != "" {
			mods[id] = true
		}
	}
	r, err := crowdplay.NewRunner(crowdplay.Account, crowdGame{src}, crowdplay.Options{Moderators: mods,
		Reply: func(text string) {
			go func() {
				if err := d.Reply(ctx, text); err != nil {
					log.Printf("controlpanel: CrowdPlay reply failed: %v", err)
				}
			}()
		},
		Log: func(line string) { log.Printf("controlpanel: %s", line) }})
	if err != nil {
		fail(err)
		return
	}
	log.Printf("controlpanel: CrowdPlay on: Discord channel %s drives %q only (bank/trade/drop/give refused)", *crowdChannel, crowdplay.Account)
	go r.Loop(ctx)
	go func() {
		if err := d.Run(ctx, r.OnMessage); err != nil && ctx.Err() == nil {
			log.Printf("controlpanel: CrowdPlay stopped: %v", err)
		}
	}()
}
