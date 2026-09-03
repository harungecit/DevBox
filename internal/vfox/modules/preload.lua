-- Helpers every vfox plugin may rely on (vfox ships printTable in its own
-- preload script) plus DevBox's io.popen replacement: the real io.popen would
-- spawn a visible console window inside a GUI app and its output would be
-- lost, so commands run through __devbox_popen (hidden window, captured) and
-- the result is exposed through a small file-like table.
function printTable(t, indent)
    indent = indent or 0
    local strIndent = string.rep("  ", indent)
    for key, value in pairs(t) do
        local keyStr = tostring(key)
        local valueStr = tostring(value)
        if type(value) == "table" then
            print(strIndent .. "[" .. keyStr .. "] =>")
            printTable(value, indent + 1)
        else
            print(strIndent .. "[" .. keyStr .. "] => " .. valueStr)
        end
    end
end

io.popen = function(cmd, mode)
    if mode ~= nil and mode ~= "r" then
        error("io.popen: only read mode is supported in DevBox")
    end
    local out, code = __devbox_popen(cmd)
    local pos = 1
    local f = {}
    local function readLine(keepNewline)
        if pos > #out then return nil end
        local nl = out:find("\n", pos, true)
        local line
        if nl then
            line = out:sub(pos, nl - 1)
            pos = nl + 1
            if keepNewline then line = line .. "\n" end
        else
            line = out:sub(pos)
            pos = #out + 1
        end
        if line:sub(-1) == "\r" and not keepNewline then line = line:sub(1, -2) end
        return line
    end
    function f:read(fmt)
        fmt = fmt or "*l"
        if type(fmt) == "number" then
            if pos > #out then return nil end
            local s = out:sub(pos, pos + fmt - 1)
            pos = pos + fmt
            return s
        end
        if fmt == "*a" or fmt == "a" then
            local s = out:sub(pos)
            pos = #out + 1
            return s
        end
        if fmt == "*l" or fmt == "l" then return readLine(false) end
        if fmt == "*L" or fmt == "L" then return readLine(true) end
        if fmt == "*n" or fmt == "n" then
            local s = out:match("^%s*[-+]?%d*%.?%d+", pos)
            if not s then return nil end
            pos = pos + #s
            return tonumber(s)
        end
        return nil
    end
    function f:lines()
        return function() return readLine(false) end
    end
    function f:close()
        if code == 0 then return true, "exit", 0 end
        return nil, "exit", code
    end
    return f
end
