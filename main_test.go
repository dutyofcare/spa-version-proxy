package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAppRewrite(t *testing.T) {
	for _, tc := range []struct {
		in     string
		expect string
	}{
		{in: "/"},
		{in: "/index.html"},
		{in: "/style.css", expect: "/style.css"},
		{in: "/billing.html", expect: "/billing.html"},
		{in: "/sub/app"},
		{in: "/sub/app/"},
	} {
		if tc.expect == "" {
			tc.expect = "/index.html"
		}
		rw := httptest.NewRecorder()
		req := httptest.NewRequest("GET", tc.in, nil)
		AppRewrite(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			if req.URL.Path != tc.expect {
				t.Errorf("Rewrite %s got %s, Expect %s", tc.in, req.URL.Path, tc.expect)
			}
		})).ServeHTTP(rw, req)
	}
}

func TestVersionSwitch(t *testing.T) {
	versionSwitch := VersionSwitch(func() string { return "default" })

	for _, tc := range []struct {
		name         string
		reqCookie    *http.Cookie
		reqPath      string
		expectPath   string
		expectCookie string
	}{
		{
			name:       "Default",
			reqPath:    "/index.html",
			expectPath: "/default/index.html",
		},
		{
			name:         "Query String",
			reqPath:      "/index.html?version=v1",
			expectPath:   "/v1/index.html",
			expectCookie: "v1",
		},
		{
			name:    "Cookie",
			reqPath: "/style.css",
			reqCookie: &http.Cookie{
				Name:  VersionCookieName,
				Value: "v1",
			},
			expectPath:   "/v1/style.css",
			expectCookie: "v1",
		},
		{
			name:    "Query Trumps Cookie",
			reqPath: "/style.css?version=v2",
			reqCookie: &http.Cookie{
				Name:  VersionCookieName,
				Value: "v1",
			},
			// QS Wins over Cookie
			expectPath:   "/v2/style.css",
			expectCookie: "v2",
		},
		{
			name:         "Parent Path Injection",
			reqPath:      "/index.html?version=..",
			expectPath:   "/index.html",
			expectCookie: "..",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rw := httptest.NewRecorder()
			req := httptest.NewRequest("GET", tc.reqPath, nil)
			if tc.reqCookie != nil {
				req.AddCookie(tc.reqCookie)
			}
			versionSwitch(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
				if req.URL.Path != tc.expectPath {
					t.Errorf("Rewrite %s got %s, Expect %s", tc.reqPath, req.URL.Path, tc.expectPath)
				}
			})).ServeHTTP(rw, req)

			cookies := rw.Result().Cookies()
			var matchingCookie *http.Cookie
			for _, cookie := range cookies {
				if cookie.Name == VersionCookieName {
					matchingCookie = cookie
				}
			}

			if tc.expectCookie == "" {
				if matchingCookie != nil {
					t.Errorf("Expected no cookie, got one")
				}
			} else {
				if matchingCookie == nil {
					t.Fatalf("Expected cookie, got none")
				}

				if matchingCookie.Value != tc.expectCookie {
					t.Fatalf("Expected cookie version was %s, Want %s", matchingCookie.Value, tc.expectCookie)
				}

			}
		})
	}
}
