package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCheckURL_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := srv.Client()
	got := checkURL(client, srv.URL)
	if got != "ok" {
		t.Errorf("checkURL() = %q, want %q", got, "ok")
	}
}

func TestCheckURL_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := srv.Client()
	got := checkURL(client, srv.URL)
	if got != "HTTP 404" {
		t.Errorf("checkURL() = %q, want %q", got, "HTTP 404")
	}
}

func TestCheckURL_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := srv.Client()
	got := checkURL(client, srv.URL)
	if got != "HTTP 500" {
		t.Errorf("checkURL() = %q, want %q", got, "HTTP 500")
	}
}

func TestCheckURL_HeadRejectedFallsBackToGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := srv.Client()
	got := checkURL(client, srv.URL)
	if got != "ok" {
		t.Errorf("checkURL() with HEAD 405 fallback = %q, want %q", got, "ok")
	}
}

func TestCheckURL_ForbiddenFallsBackToGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := srv.Client()
	got := checkURL(client, srv.URL)
	if got != "ok" {
		t.Errorf("checkURL() with HEAD 403 fallback = %q, want %q", got, "ok")
	}
}

func TestCheckURL_FallbackGetAlsoFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := srv.Client()
	got := checkURL(client, srv.URL)
	if got != "HTTP 404" {
		t.Errorf("checkURL() fallback fail = %q, want %q", got, "HTTP 404")
	}
}

func TestCheckURL_ConnectionError(t *testing.T) {
	client := &http.Client{}
	got := checkURL(client, "http://127.0.0.1:1") // nothing listening
	if got == "ok" {
		t.Error("checkURL() = ok for unreachable host, want error")
	}
}

func TestCheckURL_InvalidURL(t *testing.T) {
	client := &http.Client{}
	got := checkURL(client, "://bad-url")
	if got == "ok" {
		t.Error("checkURL() = ok for invalid URL, want error")
	}
}

func TestCheckURL_SetsUserAgent(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := srv.Client()
	checkURL(client, srv.URL)
	if gotUA != "kaddons-linkcheck/1.0" {
		t.Errorf("User-Agent = %q, want %q", gotUA, "kaddons-linkcheck/1.0")
	}
}
