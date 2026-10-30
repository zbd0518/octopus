use extism_pdk::{plugin_fn, FnResult, Json};
use octopus_plugin::{continue_with, observe, reply, Request};

#[plugin_fn]
pub fn handle(Json(request): Json<Request>) -> FnResult<Vec<u8>> {
    let response = match request.event.as_str() {
        "relay.request.before_route" => continue_with(request.payload),
        "http.rpc" => continue_with(serde_json::json!({"plugin": request.plugin_id, "runtime": "rust"})),
        _ => observe(),
    };
    reply(&response).map_err(Into::into)
}
