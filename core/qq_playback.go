package core

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/guohuiyuan/music-lib/model"
)

const qqPlaybackEndpoint = "https://u.y.qq.com/cgi-bin/musicu.fcg"

type qqPlaybackPostFunc func(payload []byte, cookie string, gtk string, uin string) ([]byte, error)

var (
	qqPlaybackHTTPClient                    = &http.Client{Timeout: 10 * time.Second}
	qqPlaybackPost       qqPlaybackPostFunc = postQQPlaybackRequest
)

// GetQQPlaybackURL resolves a QQ Music stream URL using the same vkey endpoint
// shape as the official H5 player. It is intentionally separate from the
// download resolver because QQ treats playback and file download differently.
func GetQQPlaybackURL(song *model.Song) (string, error) {
	if song == nil {
		return "", errors.New("qq playback requires song")
	}
	if song.Source != "qq" {
		return "", errors.New("source mismatch")
	}

	songMID := qqPlaybackSongMID(song)
	if songMID == "" {
		return "", errors.New("qq playback requires songmid")
	}

	cookie := CM.Get("qq")
	uin := qqPlaybackUIN(cookie)
	loginFlag := 0
	if qqPlaybackHasLoginToken(cookie) {
		loginFlag = 1
	}

	reqData := map[string]interface{}{
		"comm": map[string]interface{}{
			"ct":     23,
			"cv":     0,
			"format": "json",
			"uin":    uin,
		},
		"req_1": map[string]interface{}{
			"module": "vkey.GetVkeyServer",
			"method": "CgiGetVkey",
			"param": map[string]interface{}{
				"guid":      qqPlaybackGUID(cookie),
				"songmid":   []string{songMID},
				"songtype":  []int{0},
				"uin":       uin,
				"loginflag": loginFlag,
				"platform":  "23",
			},
		},
	}

	jsonData, err := json.Marshal(reqData)
	if err != nil {
		return "", err
	}

	body, err := qqPlaybackPost(jsonData, cookie, qqPlaybackGTK(cookie), uin)
	if err != nil {
		return "", err
	}

	var result qqPlaybackVKeyResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("qq playback json parse error: %w", err)
	}

	for _, info := range result.Req1.Data.MidURLInfo {
		if strings.TrimSpace(info.PURL) != "" {
			return qqPlaybackJoinURL(result.Req1.Data.PlaybackBaseURL(), info.PURL), nil
		}
	}

	return "", fmt.Errorf("qq playback url unavailable (code %d, req code %d)", result.Code, result.Req1.Code)
}

type qqPlaybackVKeyResponse struct {
	Code int `json:"code"`
	Req1 struct {
		Code int                `json:"code"`
		Data qqPlaybackVKeyData `json:"data"`
	} `json:"req_1"`
}

type qqPlaybackVKeyData struct {
	ThirdIP    []string `json:"thirdip"`
	SIP        []string `json:"sip"`
	MidURLInfo []struct {
		SongMID  string `json:"songmid"`
		Filename string `json:"filename"`
		PURL     string `json:"purl"`
		VKey     string `json:"vkey"`
		Code     int    `json:"code"`
	} `json:"midurlinfo"`
}

func (data qqPlaybackVKeyData) PlaybackBaseURL() string {
	for _, base := range data.ThirdIP {
		if strings.TrimSpace(base) != "" {
			return strings.TrimSpace(base)
		}
	}
	for _, base := range data.SIP {
		if strings.TrimSpace(base) != "" {
			return strings.TrimSpace(base)
		}
	}
	return "https://dl.stream.qqmusic.qq.com/"
}

func postQQPlaybackRequest(payload []byte, cookie string, gtk string, uin string) ([]byte, error) {
	endpointURL, err := url.Parse(qqPlaybackEndpoint)
	if err != nil {
		return nil, err
	}
	query := endpointURL.Query()
	query.Set("format", "json")
	if gtk != "" {
		query.Set("g_tk", gtk)
	}
	if uin != "" {
		query.Set("uin", uin)
	}
	endpointURL.RawQuery = query.Encode()

	req, err := http.NewRequest(http.MethodPost, endpointURL.String(), bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UA_Common)
	req.Header.Set("Referer", "https://y.qq.com/")
	req.Header.Set("Origin", "https://y.qq.com")
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(cookie) != "" {
		req.Header.Set("Cookie", cookie)
	}

	resp, err := qqPlaybackHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if readErr != nil {
			return nil, fmt.Errorf("qq playback http status %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("qq playback http status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if readErr != nil {
		return nil, readErr
	}
	return body, nil
}

func qqPlaybackSongMID(song *model.Song) string {
	if song == nil {
		return ""
	}
	if song.Extra != nil {
		if mid := qqPlaybackFirstNonEmpty(song.Extra["songmid"], song.Extra["song_mid"], song.Extra["mid"]); mid != "" {
			return mid
		}
	}
	return strings.TrimSpace(song.ID)
}

func qqPlaybackUIN(cookie string) string {
	uin := qqPlaybackFirstNonEmpty(
		qqPlaybackCookieValue(cookie, "wxuin"),
		qqPlaybackCookieValue(cookie, "uin"),
		qqPlaybackCookieValue(cookie, "ptui_loginuin"),
		qqPlaybackCookieValue(cookie, "luin"),
		qqPlaybackCookieValue(cookie, "pt2gguin"),
		qqPlaybackCookieValue(cookie, "superuin"),
		qqPlaybackCookieValue(cookie, "p_uin"),
		qqPlaybackCookieValue(cookie, "musicid"),
		qqPlaybackCookieValue(cookie, "userid"),
	)
	uin = strings.TrimLeft(strings.TrimPrefix(strings.TrimSpace(uin), "o"), "0")
	if uin == "" {
		return "0"
	}
	return uin
}

func qqPlaybackGTK(cookie string) string {
	token := qqPlaybackFirstNonEmpty(
		qqPlaybackCookieValue(cookie, "skey"),
		qqPlaybackCookieValue(cookie, "p_skey"),
		qqPlaybackCookieValue(cookie, "qqmusic_key"),
		qqPlaybackCookieValue(cookie, "qm_keyst"),
		qqPlaybackCookieValue(cookie, "musickey"),
	)
	if token == "" {
		return "5381"
	}
	return fmt.Sprintf("%d", qqPlaybackHash33(token))
}

func qqPlaybackHasLoginToken(cookie string) bool {
	return qqPlaybackFirstNonEmpty(
		qqPlaybackCookieValue(cookie, "skey"),
		qqPlaybackCookieValue(cookie, "p_skey"),
		qqPlaybackCookieValue(cookie, "qqmusic_key"),
		qqPlaybackCookieValue(cookie, "qm_keyst"),
		qqPlaybackCookieValue(cookie, "musickey"),
		qqPlaybackCookieValue(cookie, "wxopenid"),
		qqPlaybackCookieValue(cookie, "openid"),
	) != ""
}

func qqPlaybackGUID(cookie string) string {
	guid := strings.TrimSpace(qqPlaybackCookieValue(cookie, "pgv_pvid"))
	if qqPlaybackIsDigits(guid) {
		return guid
	}
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return fmt.Sprintf("%d", r.Int63n(9000000000)+1000000000)
}

func qqPlaybackJoinURL(base string, purl string) string {
	purl = strings.TrimSpace(purl)
	if parsed, err := url.Parse(purl); err == nil && parsed.IsAbs() {
		return purl
	}
	base = strings.TrimSpace(base)
	if strings.HasPrefix(base, "//") {
		base = "https:" + base
	}
	if base == "" {
		base = "https://dl.stream.qqmusic.qq.com/"
	}
	if strings.HasPrefix(purl, "/") {
		return strings.TrimRight(base, "/") + purl
	}
	return strings.TrimRight(base, "/") + "/" + purl
}

func qqPlaybackCookieValue(cookie string, key string) string {
	for _, part := range strings.Split(cookie, ";") {
		part = strings.TrimSpace(part)
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 && strings.TrimSpace(kv[0]) == key {
			return strings.TrimSpace(kv[1])
		}
	}
	return ""
}

func qqPlaybackFirstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func qqPlaybackHash33(s string) int {
	h := 0
	for _, c := range s {
		h += (h << 5) + int(c)
	}
	return h & 0x7fffffff
}

func qqPlaybackIsDigits(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	for _, r := range value {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
