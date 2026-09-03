package project

import "strings"

// Starter file sets for frameworks whose official generators are interactive
// or need several steps. Each returns path → contents; scaffoldFilesThen
// writes them and runs the dependency install.

func starterExpress(name string) map[string]string {
	return map[string]string{
		"package.json": `{
  "name": "` + name + `",
  "private": true,
  "type": "module",
  "scripts": { "dev": "node --watch index.js", "start": "node index.js" },
  "dependencies": { "express": "^4" }
}
`,
		"index.js": `import express from "express";

const app = express();
const port = process.env.PORT || 3000;

app.get("/", (req, res) => res.send("Hello from Express on DevBox!"));

app.listen(port, "127.0.0.1", () => console.log("listening on " + port));
`,
	}
}

func starterGin(name string) map[string]string {
	return map[string]string{
		"go.mod": "module " + name + "\n\ngo 1.22\n",
		"main.go": `package main

import (
	"os"

	"github.com/gin-gonic/gin"
)

func main() {
	r := gin.Default()
	r.GET("/", func(c *gin.Context) { c.String(200, "Hello from Gin on DevBox!") })
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	r.Run("127.0.0.1:" + port)
}
`,
	}
}

func starterFlask() map[string]string {
	return map[string]string{
		"requirements.txt": "flask\n",
		"app.py": `from flask import Flask

app = Flask(__name__)


@app.get("/")
def index():
    return "Hello from Flask on DevBox!"
`,
	}
}

// starterKemal is a minimal Kemal (Crystal) app. Kemal.run reads --bind/--port
// from ARGV, which is how DevBox's dev-server command hands it the port.
func starterKemal(name string) map[string]string {
	shard := strings.ToLower(strings.ReplaceAll(name, "-", "_"))
	return map[string]string{
		"shard.yml": `name: ` + shard + `
version: 0.1.0

targets:
  ` + shard + `:
    main: src/` + shard + `.cr

dependencies:
  kemal:
    github: kemalcr/kemal

crystal: ">= 1.0.0"

license: MIT
`,
		"src/" + shard + ".cr": `require "kemal"

get "/" do
  "Hello from Kemal on DevBox!"
end

Kemal.run
`,
		".gitignore": "/lib/\n/bin/\n/.shards/\n*.dwarf\n",
	}
}

func starterFastAPI() map[string]string {
	return map[string]string{
		"requirements.txt": "fastapi\nuvicorn[standard]\n",
		"main.py": `from fastapi import FastAPI

app = FastAPI()


@app.get("/")
def index():
    return {"message": "Hello from FastAPI on DevBox!"}
`,
	}
}
