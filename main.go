package main

import (
	"encoding/json"
	"fmt"
	"github.com/bwmarrin/discordgo"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unsafe"
)

import _ "embed"

//go:embed token.txt
var dtoken string

type config struct {
	prefix string
	botID  string
	commands map[string]func(*config, *discordgo.Session, *discordgo.Message, *parsingResult)
}

var zeroPad = string([]byte{226, 128, 139})

func playground(cfg *config, s *discordgo.Session, m *discordgo.Message, res *parsingResult) {
	needHelp := findBoolOption(res.options, "help", "h")
	if needHelp {
		help(cfg, s, m, res)

		return
	}

	debug := findBoolOption(res.options, "debug", "d", "explain", "e")
	b, err := CompileAndRun(res.content, debug)
	if err != nil {
		sendDeletable(s, m, fmt.Sprintf("```\n%v```", err), 5 * time.Minute)
	}

	var response playgroundResponse

	err = json.Unmarshal(b, &response)
	if err != nil {
		log.Println(err)

		return
	}

	if len(response.Errors) > 0 && len(response.Events) == 0 {
		sendDeletable(s, m, fmt.Sprintf("```go\n%v```", response.Errors), 5 * time.Minute)

		return
	}

	plain := findBoolOption(res.options, "plain", "p")
	if plain {
		result := ""
		for _, e := range response.Events {
			if len(result) >= 2000 {
				break
			}

			if !isPrintable(e.Message) {
				continue
			}

			result = fmt.Sprintf("%s\n_%s_\n%s\n", result, e.Kind, e.Message)
		}

		if len(response.Events) == 0 {
			result = "There's nothing to print out.\nReact with 😐 to delete this message."
		}

		if len(response.Errors) > 0 {
			result  = response.Errors + result
		}

		const plainOutputTempalte = "*Result*:\n```\n%s\n```"

		if len(result) > 2000 - len(plainOutputTempalte) {
			result = result[:2000 - len(plainOutputTempalte)]
		}

		result = fmt.Sprintf(plainOutputTempalte, result)


		sendDeletable(s, m, result, 5 * time.Minute)

		return
	}

	length := 0
	emb := &discordgo.MessageEmbed{
		Title: "Result:",
	}

	length += len("Result:")

	for _, e := range response.Events {
		switch {
		case len(e.Message) > 1024:
			e.Message = e.Message[:1024]
		case len(e.Message) == 0:
			continue
		}

		if !isPrintable(e.Message) {
			continue
		}

		length += len(e.Kind)
		length += len(e.Message)

		if length > 6000 - len("Message is too long...") {
			emb.Description = "Message is too long..."
			length += len(e.Message)

			break
		}

		emb.Fields = append(emb.Fields, &discordgo.MessageEmbedField{
			Name:  e.Kind,
			Value: e.Message,
		})

		if len(emb.Fields) == 25 {
			emb.Description = "The maximum field amount is 25.\nThe result will be cut off..."

			break
		}
	}

	if len(response.Errors) > 0 && len(response.Errors) + length < 6000 {
		emb.Description = fmt.Sprintf("```go\n%s\n```", response.Errors)
	}

	if len(response.Events) == 0 {
		emb.Fields = append(emb.Fields, &discordgo.MessageEmbedField{
			Name:  "success",
			Value: "There's nothing to print out.\nReact with 😐 to delete this message.",
		})
	}

	sendDeletable(s, m, emb, 5 * time.Minute)
}

func commandHandler(cfg *config, s *discordgo.Session, msg interface{}) func() {
	var (
		content string
		pmsg unsafe.Pointer
		bot = true
	)

	switch m := msg.(type) {
	case *discordgo.MessageCreate:
		pmsg = unsafe.Pointer(m)
		content = m.Content
		bot = m.Author.Bot
	case *discordgo.MessageUpdate:
		pmsg = unsafe.Pointer(m)
		content = m.Content
		if m.Author != nil {
			bot = m.Author.Bot
		} else {
			bot = false
		}
	default:

	}

	if bot {
		return nil
	}

	content = catchPrefix(content, cfg.prefix, cfg.botID)
	if content == "" {
		return nil
	}

	res := parseCommand(content, " \t\n", []string{"-", "--"}, []string{"="})
	if len(res.command) < 1 {
		return nil
	}

	command, ok := cfg.commands[res.command]
	if !ok  {
		return nil
	}

	return func() {
		m := *(**discordgo.Message)(pmsg)
		command(cfg, s, m, res)
	}

}

func help(cfg *config, s *discordgo.Session, m *discordgo.Message, res *parsingResult) {
	sendDeletable(s, m, "```\nNo help, no hope, human. But if you like, just write it down yourself and tag @English Learner, they're in charge on me.\n" +
		"Well, basically, I evaluate a code, then give the result of it and stuff. Use go command and get them!\n" +
		"Btw, react with 😐 within 5 mins to rid of anything I reply to you.\n```", 5 * time.Minute)
}

func main() {
	cfg := &config{
		prefix: "!",
	}

	cfg.commands = make(map[string]func(*config, *discordgo.Session, *discordgo.Message, *parsingResult))
	cfg.commands["go"] = playground
	cfg.commands["help"] = help
	cfg.commands["source"] = func(c *config, session *discordgo.Session, create *discordgo.Message, result *parsingResult) {
		sendDeletable(session, create, "```\nhttps://github.com/LaevusDexter/go-playground-bot```", 5 * time.Minute)
	}

	cfg.commands["invite"] = func(cfg *config, s *discordgo.Session, m *discordgo.Message, res *parsingResult) {
		sendDeletable(s, m, "https://discord.com/api/oauth2/authorize?client_id=486297649490952192&permissions=0&scope=bot", 5 * time.Minute)
	}


	cfg.commands["clear"] = func(cfg *config, s *discordgo.Session, m *discordgo.Message, res *parsingResult) {
		if !hasRoleName(s, m.GuildID, m.Author.ID, "Gopher Herder") {
			return
		}

		msgs, err := s.ChannelMessages(m.ChannelID, 100, m.ID, "", "")
		if err != nil {
			log.Println(err)

			return
		}

		arg := strings.TrimSpace(res.content)
	 	num := 1
	 	if arg != "" {
			num, err = strconv.Atoi(arg)
			if err != nil {
				log.Println(err)
			}
		}

		if num <= 0 {
			return
		}

		dmsgs := make([]string, 0, 2)
		for _, msg := range msgs {
			if len(dmsgs) >= num {
				break
			}

			if msg.Author.ID == cfg.botID {
				dmsgs = append(dmsgs, msg.ID)
			}
		}

		for _, dmsg := range dmsgs {
			err = s.ChannelMessageDelete(m.ChannelID, dmsg)
			if err != nil {
				log.Println("ChannelMessageDelete: ", err)
			}
		}

		if len(dmsgs) > 0 {
			s.MessageReactionAdd(m.ChannelID, m.ID, "😐")
		}
	}

	dg, err := discordgo.New("Bot " + strings.TrimSpace(dtoken))
	if err != nil {
		log.Println(err)

		return
	}

	dg.AddHandler(func (s *discordgo.Session, m *discordgo.MessageCreate) {
		command := commandHandler(cfg, s, m)
		if command == nil {
			return
		}

		command()
	})

	dg.AddHandler(func (s *discordgo.Session, m *discordgo.MessageUpdate) {
		command := commandHandler(cfg, s, m)
		if command == nil {
			return
		}

		command()
	})

	dg.AddHandler(func (s *discordgo.Session, r *discordgo.Ready) {
		cfg.botID = r.User.ID
		log.Println("cfg.botID = ", r.User.ID)
	})

	dg.AddHandler(func (s *discordgo.Session, gc *discordgo.GuildCreate) {
		s.State.GuildAdd(gc.Guild)
	})

	dg.StateEnabled = true
	dg.State.TrackVoice = false
	dg.State.TrackChannels = false
	dg.State.TrackEmojis = false
	dg.State.TrackPresences = false
	dg.State.TrackMembers = false
	dg.State.TrackRoles = true

	err = dg.Open()
	if err != nil {
		log.Println(err)

		return
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)

	<-sig
}

func findBoolOption(m map[string]interface{},  variants ...string) bool {
	for _, v := range variants {
		r, ok := m[v].(bool)
		if ok {
			return r
		}
	}

	return false
}

func sendDeletable(s *discordgo.Session, ctx *discordgo.Message, content interface{}, delay time.Duration) {
	var (
		msg *discordgo.Message
		err error
	)
	
	ref := &discordgo.MessageReference{
		MessageID: ctx.ID,
		ChannelID: ctx.ChannelID,
		GuildID:   ctx.GuildID,
	}

	switch c := content.(type) {
	case string:
		msg, err = s.ChannelMessageSendComplex(ctx.ChannelID, &discordgo.MessageSend{
			Content:   c,
			Reference: ref,
		})
	case *discordgo.MessageEmbed:
		msg, err = s.ChannelMessageSendComplex(ctx.ChannelID, &discordgo.MessageSend{
			Embed:   c,
			Reference: ref,
		})
	default:
		return
	}

	if err != nil {
		log.Println("sendDeletable:", err)

		return
	}

	var (
		cancel1 ,cancel2, cancel3 func()
	)

	mtx := &sync.Mutex{}
	canceled := false
	cancelAll := func() bool {
		mtx.Lock()
		defer mtx.Unlock()

		if !canceled {
			cancel1()
			cancel2()
			cancel3()

			canceled = true

			return !canceled
		}

		return canceled
	}

	mtx.Lock()
	defer mtx.Unlock()

	votes := 0

	cancel1 = s.AddHandler(func (s *discordgo.Session, r *discordgo.MessageReactionAdd) {
		if r.MessageID == msg.ID && r.MessageReaction.Emoji.Name == "😐" {
			votes++

			if ctx.Author.ID != r.UserID && votes != 3 && !hasRoleName(s, ctx.GuildID, r.UserID, "Gopher Herder") {
				return
			}

			if cancelAll() {
				return
			}

			time.Sleep(3 * time.Second)
			s.ChannelMessageDelete(r.ChannelID, r.MessageID)
		}
	})

	cancel2 = s.AddHandler(func (s *discordgo.Session, m *discordgo.MessageUpdate) {
		if m.Message.ID == ctx.ID {
			if cancelAll() {
				return
			}

			s.ChannelMessageDelete(msg.ChannelID, msg.ID)
		}
	})

	cancel3 = s.AddHandler(func (s *discordgo.Session, m *discordgo.MessageDelete) {
		if m.Message.ID == ctx.ID {
			if cancelAll() {
				return
			}

			s.ChannelMessageDelete(msg.ChannelID, msg.ID)
		}
	})

	time.AfterFunc(delay, func() {
		cancelAll()
	})
}

func hasRole(s *discordgo.Session, guildID, userID, roleID string) bool {
	member, err := s.GuildMember(guildID, userID)
	if err != nil {
		log.Println("hasRole:", err)

		return false
	}

	for _, rid := range member.Roles {
		if roleID == rid {
			return true
		}
	}

	return false
}

func hasRoleName(s *discordgo.Session, guildID, userID, roleName string) bool {
	member, err := s.GuildMember(guildID, userID)
	if err != nil {
		log.Println("hasRoleName(session.member):", err)

		return false
	}

	for _, rid := range member.Roles {
		role, err := s.State.Role(guildID, rid)
		if err != nil {
			log.Println("hasRoleName(state.role):", err)

			return false
		}

		if role.Name == roleName {
			return true
		}
	}

	return false
}

func isPrintable(content string) bool {
	for _, r := range content {
		if unicode.IsPrint(r) {
			return true
		}
	}

	return false
}
