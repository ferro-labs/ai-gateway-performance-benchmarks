-- wrk Lua script — peak RPS measurement for any OpenAI-compatible gateway
--
-- Sends POST /v1/chat/completions with a minimal fixed payload.
-- Designed to find the maximum sustainable requests/second — pair this with
-- the local mock server so results reflect gateway overhead only.
--
-- Usage:
--   wrk -t4 -c100 -d60s -s wrk/chat_completions.lua http://localhost:8080
--
-- High-VU peak test (mirrors k6 peak_5k but single-process):
--   wrk -t12 -c500 -d60s -s wrk/chat_completions.lua http://localhost:8080
--
-- Environment:
--   GATEWAY_URL is set via the positional arg to wrk (last CLI argument).
--   API key: edit the Authorization header below, or leave empty for the mock.

local api_key = os.getenv("API_KEY") or ""
local model   = os.getenv("MODEL") or "gpt-4o-mini"

wrk.method = "POST"
wrk.path   = "/v1/chat/completions"

wrk.headers["Content-Type"]  = "application/json"
if api_key ~= "" then
    wrk.headers["Authorization"] = "Bearer " .. api_key
end

wrk.body = string.format(
    '{"model":"%s","messages":[{"role":"user","content":"Return one word."}],"max_tokens":4}',
    model
)

-- Per-request response validation: log non-200 responses to stderr.
function response(status, headers, body)
    if status ~= 200 then
        io.stderr:write(string.format("[wrk] non-200 response: %d  body: %s\n", status, body:sub(1, 120)))
    end
end
