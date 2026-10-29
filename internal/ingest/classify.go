package ingest

import (
	"go.mau.fi/whatsmeow/proto/waE2E"

	"github.com/ribeirogab/zapgate/internal/store"
)

type content struct {
	kind     string
	text     string
	quotedID string
	media    *store.Media
	details  *store.Details
}

type mediaMessage interface {
	GetMimetype() string
	GetFileLength() uint64
	GetMediaKey() []byte
	GetDirectPath() string
	GetFileSHA256() []byte
	GetFileEncSHA256() []byte
	GetContextInfo() *waE2E.ContextInfo
}

func classify(msg *waE2E.Message) (content, bool) {
	for _, try := range []func(*waE2E.Message) (content, bool){classifyText, classifyMedia, classifyShared} {
		if found, ok := try(msg); ok {
			return found, true
		}
	}
	return content{}, false
}

func classifyText(msg *waE2E.Message) (content, bool) {
	switch {
	case msg.GetConversation() != "":
		return content{kind: store.MessageText, text: msg.GetConversation()}, true
	case msg.GetExtendedTextMessage() != nil:
		ext := msg.GetExtendedTextMessage()
		return content{kind: store.MessageText, text: ext.GetText(), quotedID: ext.GetContextInfo().GetStanzaID()}, true
	}
	return content{}, false
}

func classifyMedia(msg *waE2E.Message) (content, bool) {
	switch {
	case msg.GetImageMessage() != nil:
		img := msg.GetImageMessage()
		return withMedia(store.MessageImage, img.GetCaption(), img, store.Media{}), true
	case msg.GetVideoMessage() != nil:
		vid := msg.GetVideoMessage()
		return withMedia(store.MessageVideo, vid.GetCaption(), vid, store.Media{Seconds: int(vid.GetSeconds())}), true
	case msg.GetPtvMessage() != nil:
		vid := msg.GetPtvMessage()
		return withMedia(store.MessageVideo, vid.GetCaption(), vid, store.Media{Seconds: int(vid.GetSeconds())}), true
	case msg.GetAudioMessage() != nil:
		audio := msg.GetAudioMessage()
		return withMedia(store.MessageAudio, "", audio, store.Media{Seconds: int(audio.GetSeconds()), VoiceNote: audio.GetPTT()}), true
	case msg.GetDocumentMessage() != nil:
		doc := msg.GetDocumentMessage()
		return withMedia(store.MessageDocument, doc.GetCaption(), doc, store.Media{FileName: doc.GetFileName()}), true
	case msg.GetStickerMessage() != nil:
		return withMedia(store.MessageSticker, "", msg.GetStickerMessage(), store.Media{}), true
	}
	return content{}, false
}

func withMedia(kind, caption string, file mediaMessage, extra store.Media) content {
	extra.Mime = file.GetMimetype()
	extra.Size = int64(file.GetFileLength())
	extra.MediaKey = file.GetMediaKey()
	extra.DirectPath = file.GetDirectPath()
	extra.FileSHA256 = file.GetFileSHA256()
	extra.FileEncSHA256 = file.GetFileEncSHA256()
	return content{kind: kind, text: caption, quotedID: file.GetContextInfo().GetStanzaID(), media: &extra}
}

func classifyShared(msg *waE2E.Message) (content, bool) {
	switch {
	case msg.GetLocationMessage() != nil:
		loc := msg.GetLocationMessage()
		place := store.Location{Latitude: loc.GetDegreesLatitude(), Longitude: loc.GetDegreesLongitude(), Name: loc.GetName(), Address: loc.GetAddress(), URL: loc.GetURL(), Live: loc.GetIsLive()}
		return content{kind: store.MessageLocation, text: firstText(loc.GetName(), loc.GetComment()), quotedID: loc.GetContextInfo().GetStanzaID(), details: &store.Details{Location: &place}}, true
	case msg.GetLiveLocationMessage() != nil:
		loc := msg.GetLiveLocationMessage()
		place := store.Location{Latitude: loc.GetDegreesLatitude(), Longitude: loc.GetDegreesLongitude(), Live: true}
		return content{kind: store.MessageLocation, text: loc.GetCaption(), quotedID: loc.GetContextInfo().GetStanzaID(), details: &store.Details{Location: &place}}, true
	case msg.GetContactMessage() != nil:
		card := msg.GetContactMessage()
		cards := []store.ContactCard{{Name: card.GetDisplayName(), VCard: card.GetVcard()}}
		return content{kind: store.MessageContact, text: card.GetDisplayName(), quotedID: card.GetContextInfo().GetStanzaID(), details: &store.Details{Contacts: cards}}, true
	case msg.GetContactsArrayMessage() != nil:
		return contactList(msg.GetContactsArrayMessage()), true
	case pollOf(msg) != nil:
		return poll(pollOf(msg)), true
	}
	return content{}, false
}

func contactList(list *waE2E.ContactsArrayMessage) content {
	cards := make([]store.ContactCard, 0, len(list.GetContacts()))
	for _, card := range list.GetContacts() {
		cards = append(cards, store.ContactCard{Name: card.GetDisplayName(), VCard: card.GetVcard()})
	}
	return content{kind: store.MessageContact, text: list.GetDisplayName(), quotedID: list.GetContextInfo().GetStanzaID(), details: &store.Details{Contacts: cards}}
}

func poll(creation *waE2E.PollCreationMessage) content {
	options := make([]string, 0, len(creation.GetOptions()))
	for _, option := range creation.GetOptions() {
		options = append(options, option.GetOptionName())
	}
	details := &store.Details{Poll: &store.Poll{Options: options, MaxChoices: int(creation.GetSelectableOptionsCount())}}
	return content{kind: store.MessagePoll, text: creation.GetName(), quotedID: creation.GetContextInfo().GetStanzaID(), details: details}
}

func pollOf(msg *waE2E.Message) *waE2E.PollCreationMessage {
	for _, creation := range []*waE2E.PollCreationMessage{
		msg.GetPollCreationMessage(),
		msg.GetPollCreationMessageV2(),
		msg.GetPollCreationMessageV3(),
		msg.GetPollCreationMessageV5(),
		msg.GetPollCreationMessageV6(),
		msg.GetPollCreationMessageV4().GetMessage().GetPollCreationMessage(),
	} {
		if creation != nil {
			return creation
		}
	}
	return nil
}

func firstText(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
