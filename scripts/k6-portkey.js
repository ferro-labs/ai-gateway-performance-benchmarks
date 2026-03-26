import http from 'k6/http';

export const options = {
  vus: parseInt(__ENV.VUS || '150'),
  duration: __ENV.DURATION || '60s',
};

export default function () {
  http.post(
    'http://localhost:8787/v1/chat/completions',
    JSON.stringify({
      model: 'gpt-4o',
      messages: [{ role: 'user', content: 'hello' }],
      stream: false,
    }),
    {
      headers: {
        'Content-Type': 'application/json',
        'Authorization': 'Bearer benchmark-portkey-key',
        'x-portkey-provider': 'openai',
        'x-portkey-custom-host': 'http://localhost:9000/v1',
      },
      timeout: '10s',
    }
  );
}
