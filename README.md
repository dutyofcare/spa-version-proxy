![GitHub](https://img.shields.io/github/license/dutyofcare/spa-version-proxy.go)

Single Page App Version Proxy
==============================

A reverse proxy for Single Page JS apps, e.g. react, with a focus on Create
React App compatibility

Set up a web server holding multiple versions as `/versions/{version}/`

Put the 'default' version as a string in `/default-version.txt`

Users can request any version with ?version={version}, otherwise the default
version will be served.

The version will be stored in a cookie so that resources loaded by HTML pages
are also versioned (css, js etc)

All requests without an extension, including directories, are assumed to be the
app, so `/index.html` will be served
