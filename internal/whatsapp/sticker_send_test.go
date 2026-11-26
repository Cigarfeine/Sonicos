package whatsapp

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"testing"

	"github.com/sonicos/whatsapp-bot/internal/command"
	"github.com/sonicos/whatsapp-bot/internal/sticker"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

func buildTestUploadResponse(plain []byte, keyFill byte, path string) whatsmeow.UploadResponse {
	mediaKey := bytes.Repeat([]byte{keyFill}, 32)
	plainSha := sha256.Sum256(plain)
	expanded, _ := hkdf.Key(sha256.New, mediaKey, nil, string(whatsmeow.MediaImage), 112)
	iv, cipherKey, macKey := expanded[0:16], expanded[16:48], expanded[48:80]
	block, _ := aes.NewCipher(cipherKey)
	padded := pkcs7Pad(plain, aes.BlockSize)
	ct := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ct, padded)

	macH := hmac.New(sha256.New, macKey)
	macH.Write(iv)
	macH.Write(ct)
	fullMac := macH.Sum(nil)[:10]

	shaH := sha256.New()
	shaH.Write(ct)
	shaH.Write(fullMac)
	encSha := shaH.Sum(nil)

	return whatsmeow.UploadResponse{
		URL:           "https://mmg.whatsapp.net" + path,
		DirectPath:    path,
		MediaKey:      mediaKey,
		FileSHA256:    plainSha[:],
		FileEncSHA256: encSha,
		FileLength:    uint64(len(plain)),
	}
}

func buildTestWebpWithExif(t *testing.T, frameLen int) ([]byte, uint32) {
	t.Helper()
	vp8x := make([]byte, 18)
	copy(vp8x[0:4], "VP8X")
	binary.LittleEndian.PutUint32(vp8x[4:8], 10)
	binary.LittleEndian.PutUint32(vp8x[8:12], 0x02) // ANIM

	payload := bytes.Repeat([]byte{0x55}, frameLen)
	padded := len(payload) + (len(payload) & 1)
	anmf := make([]byte, 8+padded)
	copy(anmf[0:4], "ANMF")
	binary.LittleEndian.PutUint32(anmf[4:8], uint32(len(payload)))
	copy(anmf[8:], payload)

	body := append(vp8x, anmf...)
	riff := make([]byte, 12)
	copy(riff[0:4], "RIFF")
	binary.LittleEndian.PutUint32(riff[4:8], uint32(4+len(body)))
	copy(riff[8:12], "WEBP")
	raw := append(riff, body...)

	out, off, err := sticker.InjectWebpExif(raw, sticker.ExifMetadata{
		PackID:    "com.sonicos.sound.test",
		PackName:  "Sonicos Sound",
		Publisher: "Sonicos",
		Emojis:    []string{"🔊"},
	}, 512, 512)
	if err != nil {
		t.Fatalf("InjectWebpExif: %v", err)
	}
	return out, off
}

func TestComputeFirstFrameSidecar(t *testing.T) {
	webp, off := buildTestWebpWithExif(t, 77)
	up := buildTestUploadResponse(webp, 0x2A, "/o1/v/t24/sticker.enc")

	sidecar, err := computeFirstFrameSidecar(webp, up.MediaKey, off, up.FileEncSHA256)
	if err != nil {
		t.Fatalf("computeFirstFrameSidecar: %v", err)
	}
	if len(sidecar) != 10 {
		t.Fatalf("expected 10-byte sidecar, got %d", len(sidecar))
	}
	sidecar2, _ := computeFirstFrameSidecar(webp, up.MediaKey, off, up.FileEncSHA256)
	if !bytes.Equal(sidecar, sidecar2) {
		t.Fatal("firstFrameSidecar is not deterministic")
	}

	badSha := append([]byte(nil), up.FileEncSHA256...)
	badSha[0] ^= 0xff
	if _, err := computeFirstFrameSidecar(webp, up.MediaKey, off, badSha); err == nil {
		t.Fatal("expected error on FileEncSHA256 mismatch")
	}
}

func TestBuildSoundStickerMessage_WireStructure(t *testing.T) {
	webp, off := buildTestWebpWithExif(t, 64)
	stickerUp := buildTestUploadResponse(webp, 0x11, "/v/t62.15575-24/s.enc")
	audioPlain := []byte("OggS-fake-opus")
	audioUp := whatsmeow.UploadResponse{
		URL:           "https://mmg.whatsapp.net/v/t62.7114-24/a.enc",
		DirectPath:    "/v/t62.7114-24/a.enc",
		MediaKey:      bytes.Repeat([]byte{0x22}, 32),
		FileSHA256:    bytes.Repeat([]byte{0x33}, 32),
		FileEncSHA256: bytes.Repeat([]byte{0x44}, 32),
		FileLength:    uint64(len(audioPlain)),
	}
	sidecar, err := computeFirstFrameSidecar(webp, stickerUp.MediaKey, off, stickerUp.FileEncSHA256)
	if err != nil {
		t.Fatalf("computeFirstFrameSidecar: %v", err)
	}

	wave := make([]byte, 64)
	for i := range wave {
		wave[i] = byte(i % 100)
	}
	evt := &events.Message{
		Info: types.MessageInfo{
			ID: types.MessageID("ORIG-MSG-1"),
			MessageSource: types.MessageSource{
				Chat:   types.NewJID("120363000000111", types.GroupServer),
				Sender: types.NewJID("15550001111", types.DefaultUserServer),
			},
		},
		Message: &waE2E.Message{
			VideoMessage: &waE2E.VideoMessage{
				Caption:       proto.String("!stdio"),
				JPEGThumbnail: bytes.Repeat([]byte{0xFF}, 64),
			},
		},
	}

	st := command.Sticker{
		Data:          webp,
		MIMEType:      "image/webp",
		Animated:      true,
		ExifOffset:    off,
		Emojis:        []string{"🔊", "🔥"},
		Audio:         true,
		AudioData:     audioPlain,
		AudioMIMEType: "audio/ogg; codecs=opus",
		Seconds:       5,
		Waveform:      wave,
	}

	msg, err := buildSoundStickerMessage(st, stickerUp, audioUp, off, sidecar, evt)
	if err != nil {
		t.Fatalf("buildSoundStickerMessage: %v", err)
	}

	wire, err := proto.Marshal(msg)
	if err != nil {
		t.Fatalf("proto.Marshal: %v", err)
	}

	var saw35, saw134 bool
	var f134Bytes []byte
	rem := wire
	for len(rem) > 0 {
		num, typ, tl := protowire.ConsumeTag(rem)
		if tl <= 0 {
			t.Fatal("bad tag")
		}
		rem = rem[tl:]
		if typ != protowire.BytesType {
			t.Fatalf("unexpected wire type %d for field %d", typ, num)
		}
		val, vl := protowire.ConsumeBytes(rem)
		if vl <= 0 {
			t.Fatal("bad bytes")
		}
		rem = rem[vl:]
		switch num {
		case 35:
			saw35 = true
		case 134:
			saw134 = true
			f134Bytes = val
		default:
			t.Fatalf("unexpected top-level field %d", num)
		}
	}
	if !saw35 || !saw134 {
		t.Fatalf("expected top-level fields 35 and 134, got 35=%v 134=%v", saw35, saw134)
	}
	if len(msg.GetMessageContextInfo().GetMessageSecret()) != 32 {
		t.Fatalf("expected 32-byte MessageSecret")
	}

	var fp waE2E.FutureProofMessage
	if err := proto.Unmarshal(f134Bytes, &fp); err != nil {
		t.Fatalf("unmarshal FutureProofMessage: %v", err)
	}
	decodedSt := fp.GetMessage().GetStickerMessage()
	if decodedSt == nil {
		t.Fatal("missing StickerMessage inside field 134")
	}
	if decodedSt.Width != nil || decodedSt.Height != nil {
		t.Fatalf("expected Width/Height omitted on Sound Sticker, got %v x %v", decodedSt.Width, decodedSt.Height)
	}
	if decodedSt.GetFirstFrameLength() != off || !bytes.Equal(decodedSt.GetFirstFrameSidecar(), sidecar) {
		t.Fatal("FirstFrameLength/FirstFrameSidecar mismatch")
	}
	if decodedSt.GetContextInfo().GetStanzaID() != "ORIG-MSG-1" {
		t.Fatalf("expected quoted StanzaID ORIG-MSG-1, got %q", decodedSt.GetContextInfo().GetStanzaID())
	}
	if decodedSt.GetContextInfo().GetQuotedMessage().GetVideoMessage() != nil {
		t.Fatal("expected lightweight quoted ContextInfo without bloated VideoMessage")
	}
	mdi := decodedSt.GetContextInfo().GetMediaDomainInfo()
	if mdi == nil || mdi.GetMediaKeyDomain() != waE2E.MediaKeyDomain_MEDIA_KEY_DOMAIN_E2EE || !bytes.Equal(mdi.GetE2EeMediaKey(), stickerUp.MediaKey) {
		t.Fatalf("unexpected sticker MediaDomainInfo: %+v", mdi)
	}

	nestedAudio := decodedSt.GetAudioMessage()
	if nestedAudio == nil {
		t.Fatal("expected nested AudioMessage (field 26) on StickerMessage")
	}
	if nestedAudio.GetDirectPath() != audioUp.DirectPath || nestedAudio.GetSeconds() != 5 || nestedAudio.PTT != nil {
		t.Fatalf("unexpected nested audio: path=%q sec=%d ptt=%v", nestedAudio.GetDirectPath(), nestedAudio.GetSeconds(), nestedAudio.PTT)
	}
	if len(nestedAudio.GetWaveform()) != 64 {
		t.Fatalf("expected 64-byte waveform, got %d", len(nestedAudio.GetWaveform()))
	}
	aMdi := nestedAudio.GetContextInfo().GetMediaDomainInfo()
	if aMdi == nil || aMdi.GetMediaKeyDomain() != waE2E.MediaKeyDomain_MEDIA_KEY_DOMAIN_E2EE || !bytes.Equal(aMdi.GetE2EeMediaKey(), audioUp.MediaKey) {
		t.Fatalf("unexpected audio MediaDomainInfo: %+v", aMdi)
	}
}

func TestBuildSilentStickerMessage(t *testing.T) {
	webp, off := buildTestWebpWithExif(t, 48)
	up := buildTestUploadResponse(webp, 0x31, "/v/t62.15575-24/silent.enc")
	sidecar, err := computeFirstFrameSidecar(webp, up.MediaKey, off, up.FileEncSHA256)
	if err != nil {
		t.Fatal(err)
	}
	msg := buildSilentStickerMessage(command.Sticker{
		Data:       webp,
		MIMEType:   "image/webp",
		Animated:   true,
		ExifOffset: off,
		Emojis:     []string{"✨"},
	}, up, off, sidecar, nil)

	st := msg.GetStickerMessage()
	if st == nil || st.GetWidth() != 512 || st.GetHeight() != 512 {
		t.Fatalf("expected 512x512 StickerMessage, got %+v", st)
	}
	if st.GetFirstFrameLength() != off || !bytes.Equal(st.GetFirstFrameSidecar(), sidecar) {
		t.Fatal("silent animated sticker missing FirstFrameLength/FirstFrameSidecar")
	}
	if st.GetStickerSentTS() == 0 || st.GetMediaKeyTimestamp() == 0 {
		t.Fatal("expected StickerSentTS and MediaKeyTimestamp set")
	}
}

func TestBuildStickerReactionMessage_DMAndGroup(t *testing.T) {
	webp, off := buildTestWebpWithExif(t, 48)
	up := buildTestUploadResponse(webp, 0x55, "/o1/v/t24/react.enc")
	sidecar, err := computeFirstFrameSidecar(webp, up.MediaKey, off, up.FileEncSHA256)
	if err != nil {
		t.Fatal(err)
	}

	groupJID := types.NewJID("120363000000777", types.GroupServer)
	msg, err := buildStickerReactionMessage(command.StickerReaction{
		Sticker: command.Sticker{
			Data:       webp,
			MIMEType:   "image/webp",
			Animated:   true,
			ExifOffset: off,
			Emojis:     []string{"🔥"},
		},
		TargetStanzaID:    "TARGET-STZ-99",
		TargetParticipant: "15559990001@s.whatsapp.net",
		TargetFromMe:      false,
	}, up, off, sidecar, groupJID)
	if err != nil {
		t.Fatal(err)
	}

	wire, err := proto.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	var decoded waE2E.Message
	if err := proto.Unmarshal(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.GetStickerMessage() == nil {
		t.Fatal("expected StickerMessage on sticker reaction")
	}
	assoc := decoded.GetMessageContextInfo().GetMessageAssociation()
	if assoc == nil || assoc.GetAssociationType() != waE2E.MessageAssociation_STICKER_ANNOTATION {
		t.Fatalf("expected STICKER_ANNOTATION, got %+v", assoc)
	}
	pkey := assoc.GetParentMessageKey()
	if pkey.GetID() != "TARGET-STZ-99" || pkey.GetRemoteJID() != groupJID.String() || pkey.GetParticipant() != "15559990001@s.whatsapp.net" {
		t.Fatalf("unexpected ParentMessageKey: %+v", pkey)
	}

	dmJID := types.NewJID("15559990002", types.DefaultUserServer)
	dmMsg, err := buildStickerReactionMessage(command.StickerReaction{
		Sticker:           command.Sticker{Data: webp, MIMEType: "image/webp"},
		TargetStanzaID:    "TARGET-DM-1",
		TargetParticipant: "15559990002@s.whatsapp.net",
	}, up, 0, nil, dmJID)
	if err != nil {
		t.Fatal(err)
	}
	if dmMsg.GetMessageContextInfo().GetMessageAssociation().GetParentMessageKey().Participant != nil {
		t.Fatal("expected nil Participant on DM sticker reaction")
	}
}

func TestBuildVideoNoteMessage(t *testing.T) {
	up := buildTestUploadResponse([]byte("fake-mp4"), 0x77, "/v/t62/ptv.enc")
	msg := buildVideoNoteMessage(command.VideoNoteReply{
		Data:    []byte("fake-mp4"),
		Thumb:   []byte("fake-jpg"),
		Seconds: 7,
		Width:   384,
		Height:  384,
	}, up, nil)

	ptv := msg.GetPtvMessage()
	if ptv == nil || ptv.GetWidth() != 384 || ptv.GetHeight() != 384 || ptv.GetSeconds() != 7 || ptv.GetMimetype() != "video/mp4" {
		t.Fatalf("unexpected PtvMessage: %+v", ptv)
	}
	if len(ptv.GetJPEGThumbnail()) == 0 {
		t.Fatal("expected JPEGThumbnail set on PtvMessage")
	}
}

func TestBuildVideoMessage(t *testing.T) {
	up := buildTestUploadResponse([]byte("fake-mp4"), 0x88, "/v/t62/vid.enc")
	msg := buildVideoMessage(command.VideoReply{
		Data:    []byte("fake-mp4"),
		Thumb:   []byte("fake-jpg"),
		Caption: "converted",
		Seconds: 4,
		Width:   512,
		Height:  512,
	}, up, nil)

	if msg.GetPtvMessage() != nil {
		t.Fatalf("expected nil PtvMessage on standard video reply, got %+v", msg.GetPtvMessage())
	}
	vid := msg.GetVideoMessage()
	if vid == nil || vid.GetWidth() != 512 || vid.GetHeight() != 512 || vid.GetSeconds() != 4 || vid.GetMimetype() != "video/mp4" {
		t.Fatalf("unexpected VideoMessage: %+v", vid)
	}
	if vid.GetCaption() != "converted" || len(vid.GetJPEGThumbnail()) == 0 {
		t.Fatalf("expected Caption and JPEGThumbnail set on VideoMessage, got caption=%q thumb=%d", vid.GetCaption(), len(vid.GetJPEGThumbnail()))
	}
}
