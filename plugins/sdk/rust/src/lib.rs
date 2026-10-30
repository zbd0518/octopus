use extism_pdk::{Memory, memory};
use serde::{Deserialize, Serialize, de::DeserializeOwned};
use serde_json::{Value, json};

pub const API_VERSION: u32 = 1;

#[derive(Debug, Deserialize)]
#[serde(rename_all = "snake_case")]
pub struct Request {
    pub api_version: u32,
    pub request_id: String,
    pub plugin_id: String,
    pub event: String,
    pub attempt_id: Option<String>,
    pub payload: Value,
    pub config: Option<Value>,
}

#[derive(Debug, Serialize)]
#[serde(rename_all = "snake_case")]
pub struct Response {
    pub api_version: u32,
    pub decision: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub payload: Option<Value>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub tag: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub error: Option<String>,
}

#[extism_pdk::host_fn("extism:host/user")]
extern "C" { fn host_call(offset: u64) -> u64; }

pub fn host<T: DeserializeOwned>(capability: &str, payload: Value) -> Result<T, String> {
    let input = serde_json::to_vec(&json!({"api_version": API_VERSION, "capability": capability, "payload": payload})).map_err(|e| e.to_string())?;
    let memory = Memory::from_bytes(&input).map_err(|e| e.to_string())?;
    let offset = unsafe { host_call(memory.offset()) };
    let output = Memory::new(offset).to_vec().map_err(|e| e.to_string())?;
    let response: Value = serde_json::from_slice(&output).map_err(|e| e.to_string())?;
    if response.get("ok").and_then(Value::as_bool) != Some(true) {
        return Err(response.get("error").and_then(Value::as_str).unwrap_or("host call failed").to_owned());
    }
    serde_json::from_value(response.get("payload").cloned().unwrap_or(Value::Null)).map_err(|e| e.to_string())
}

pub fn continue_with(payload: Value) -> Response {
    Response { api_version: API_VERSION, decision: "continue".into(), payload: Some(payload), tag: None, error: None }
}

pub fn observe() -> Response {
    Response { api_version: API_VERSION, decision: "observe".into(), payload: None, tag: None, error: None }
}

pub fn failure(message: impl Into<String>) -> Response {
    Response { api_version: API_VERSION, decision: "continue".into(), payload: None, tag: None, error: Some(message.into()) }
}

pub fn reply(response: &Response) -> Result<Vec<u8>, String> { serde_json::to_vec(response).map_err(|e| e.to_string()) }
