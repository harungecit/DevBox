local util = require("util")
local strings = require("vfox.strings")
local json = require("json")

function PLUGIN:Available(ctx)
    -- exercise the standard modules the way real plugins do
    assert(strings.has_prefix("abc", "a"))
    assert(json.decode('{"a":1}').a == 1)
    if ctx.args and ctx.args[1] == "hang" then
        while true do end
    end
    if ctx.args and ctx.args[1] == "exit" then
        os.exit(1)
    end
    return util.versions()
end
