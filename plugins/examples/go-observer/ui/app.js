const { pluginId, session, parentOrigin } = window.octopus;
document.getElementById('check').onclick = () => {
    const requestId = crypto.randomUUID();
    const output = document.getElementById('result');
    const timeout = setTimeout(() => {
        removeEventListener('message', handler);
        output.textContent = 'RPC timed out';
    }, 10000);
    const handler = (event) => {
        const message = event.data;
        if (event.source !== parent || event.origin !== parentOrigin ||
            message?.type !== 'octopus:plugin:rpc:v1' || message.pluginId !== pluginId ||
            message.session !== session || message.requestId !== requestId) return;
        clearTimeout(timeout);
        removeEventListener('message', handler);
        output.textContent = JSON.stringify(message.result ?? message.error, null, 2);
    };
    addEventListener('message', handler);
    parent.postMessage({ type: 'octopus:plugin:rpc:v1', pluginId, session, requestId, method: 'GET', path: 'status' }, parentOrigin);
};
