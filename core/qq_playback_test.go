package core

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/guohuiyuan/music-lib/model"
)

func TestGetQQPlaybackURLUsesOfficialPlayerVKeyRequest(t *testing.T) {
	restoreQQPlaybackPost := replaceQQPlaybackPostForTest(t, func(payload []byte, cookie string, gtk string, uin string) ([]byte, error) {
		if !strings.Contains(cookie, "uin=o0012345") {
			t.Fatalf("expected qq cookie to be forwarded, got %q", cookie)
		}
		if want := fmt.Sprintf("%d", qqPlaybackHash33("@abc")); gtk != want {
			t.Fatalf("expected gtk %s, got %s", want, gtk)
		}
		if uin != "12345" {
			t.Fatalf("expected normalized uin 12345, got %s", uin)
		}

		var req map[string]interface{}
		if err := json.Unmarshal(payload, &req); err != nil {
			t.Fatalf("request payload is not json: %v", err)
		}
		if strings.Contains(string(payload), "filename") {
			t.Fatalf("playback resolver should not send legacy filename probes: %s", payload)
		}

		req1, _ := req["req_1"].(map[string]interface{})
		if got := req1["module"]; got != "vkey.GetVkeyServer" {
			t.Fatalf("unexpected module: %v", got)
		}
		if got := req1["method"]; got != "CgiGetVkey" {
			t.Fatalf("unexpected method: %v", got)
		}
		param, _ := req1["param"].(map[string]interface{})
		if got := param["platform"]; got != "23" {
			t.Fatalf("unexpected platform: %v", got)
		}
		if got := param["uin"]; got != "12345" {
			t.Fatalf("unexpected uin: %v", got)
		}
		if got := param["loginflag"]; got != float64(1) {
			t.Fatalf("unexpected loginflag: %v", got)
		}
		if got := param["guid"]; got != "9876543210" {
			t.Fatalf("unexpected guid: %v", got)
		}
		songmids, _ := param["songmid"].([]interface{})
		if len(songmids) != 1 || songmids[0] != "003IPDsn4ZWb5H" {
			t.Fatalf("unexpected songmid payload: %v", songmids)
		}

		return []byte(`{"code":0,"req_1":{"code":0,"data":{"thirdip":["https://dl.stream.qqmusic.qq.com/"],"midurlinfo":[{"songmid":"003IPDsn4ZWb5H","purl":"C400003IPDsn4ZWb5H.m4a?vkey=test"}]}}}`), nil
	})
	defer restoreQQPlaybackPost()
	restoreCookies := replaceCookiesForTest(map[string]string{
		"qq": "uin=o0012345; skey=@abc; pgv_pvid=9876543210",
	})
	defer restoreCookies()

	got, err := GetQQPlaybackURL(&model.Song{Source: "qq", ID: "fallback", Extra: map[string]string{"songmid": "003IPDsn4ZWb5H"}})
	if err != nil {
		t.Fatalf("GetQQPlaybackURL returned error: %v", err)
	}
	want := "https://dl.stream.qqmusic.qq.com/C400003IPDsn4ZWb5H.m4a?vkey=test"
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestGetQQPlaybackURLKeepsAbsolutePURL(t *testing.T) {
	restoreQQPlaybackPost := replaceQQPlaybackPostForTest(t, func(payload []byte, cookie string, gtk string, uin string) ([]byte, error) {
		return []byte(`{"code":0,"req_1":{"code":0,"data":{"thirdip":["https://dl.stream.qqmusic.qq.com/"],"midurlinfo":[{"purl":"https://third.example/audio.m4a?vkey=test"}]}}}`), nil
	})
	defer restoreQQPlaybackPost()
	restoreCookies := replaceCookiesForTest(map[string]string{"qq": ""})
	defer restoreCookies()

	got, err := GetQQPlaybackURL(&model.Song{Source: "qq", ID: "003IPDsn4ZWb5H"})
	if err != nil {
		t.Fatalf("GetQQPlaybackURL returned error: %v", err)
	}
	if got != "https://third.example/audio.m4a?vkey=test" {
		t.Fatalf("unexpected playback url: %s", got)
	}
}

func TestGetQQPlaybackURLTreatsWXTokenAsLoggedInWithoutUIN(t *testing.T) {
	restoreQQPlaybackPost := replaceQQPlaybackPostForTest(t, func(payload []byte, cookie string, gtk string, uin string) ([]byte, error) {
		if uin != "0" {
			t.Fatalf("expected missing uin to normalize to 0, got %s", uin)
		}
		var req map[string]interface{}
		if err := json.Unmarshal(payload, &req); err != nil {
			t.Fatalf("request payload is not json: %v", err)
		}
		req1, _ := req["req_1"].(map[string]interface{})
		param, _ := req1["param"].(map[string]interface{})
		if got := param["loginflag"]; got != float64(1) {
			t.Fatalf("expected loginflag 1 for wx token, got %v", got)
		}
		return []byte(`{"code":0,"req_1":{"code":0,"data":{"midurlinfo":[{"purl":"C400003IPDsn4ZWb5H.m4a?vkey=test"}]}}}`), nil
	})
	defer restoreQQPlaybackPost()
	restoreCookies := replaceCookiesForTest(map[string]string{
		"qq": "wxopenid=openid; musickey=music-key",
	})
	defer restoreCookies()

	if _, err := GetQQPlaybackURL(&model.Song{Source: "qq", ID: "003IPDsn4ZWb5H"}); err != nil {
		t.Fatalf("GetQQPlaybackURL returned error: %v", err)
	}
}

func TestGetQQPlaybackURLErrorsWhenPURLMissing(t *testing.T) {
	restoreQQPlaybackPost := replaceQQPlaybackPostForTest(t, func(payload []byte, cookie string, gtk string, uin string) ([]byte, error) {
		return []byte(`{"code":0,"req_1":{"code":0,"data":{"midurlinfo":[{"songmid":"003IPDsn4ZWb5H","purl":""}]}}}`), nil
	})
	defer restoreQQPlaybackPost()
	restoreCookies := replaceCookiesForTest(map[string]string{"qq": ""})
	defer restoreCookies()

	if got, err := GetQQPlaybackURL(&model.Song{Source: "qq", ID: "003IPDsn4ZWb5H"}); err == nil {
		t.Fatalf("expected error for missing purl, got url %s", got)
	}
}

func replaceQQPlaybackPostForTest(t *testing.T, fn qqPlaybackPostFunc) func() {
	t.Helper()
	old := qqPlaybackPost
	qqPlaybackPost = fn
	return func() {
		qqPlaybackPost = old
	}
}

func replaceCookiesForTest(cookies map[string]string) func() {
	CM.mu.Lock()
	oldCookies := CM.cookies
	CM.cookies = cookies
	CM.mu.Unlock()

	return func() {
		CM.mu.Lock()
		CM.cookies = oldCookies
		CM.mu.Unlock()
	}
}
