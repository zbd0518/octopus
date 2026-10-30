export function buildNodeConfig(token: string, serverURL: string): string {
    const url = new URL(serverURL);
    url.protocol = 'wss:';
    url.pathname = `${url.pathname.replace(/\/$/, '')}/node/v1/connect`;
    url.search = '';
    url.hash = '';
    return JSON.stringify({
        plugin_options: {
            version: 1,
            connect: false,
            cloud_url: url.toString(),
            models: ['<MODEL_ID>'],
            max_concurrency: 4,
            request_timeout_seconds: 300,
        },
        private_config: {
            node_token: token,
            magpie_url: 'http://127.0.0.1:3425',
            magpie_key: '<LOCAL_MAGPIE_GATEWAY_KEY>',
        },
    }, null, 2);
}
