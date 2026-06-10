use base64::engine::general_purpose::STANDARD;
use base64::Engine;
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum BrokerRequest {
    Suggest { line: String },
    Submit { line: String },
    Cancel,
    HistoryLoad,
    HistoryAppend { line: String },
    Exit,
}

#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum BrokerEvent {
    Ready,
    Suggestions {
        suggestions: Vec<String>,
    },
    Started {
        batch: usize,
        index: usize,
        host: String,
        command: String,
    },
    Completed {
        batch: usize,
        index: usize,
        host: String,
        command: String,
        #[serde(with = "base64_vec")]
        stdout: Vec<u8>,
        exit_code: i32,
        error: String,
    },
    Status {
        running: usize,
        done: usize,
        failed: usize,
        pending: usize,
        total: usize,
    },
    History {
        lines: Vec<String>,
    },
    Error {
        message: String,
    },
}

mod base64_vec {
    use super::*;
    use serde::{Deserializer, Serializer};

    pub fn serialize<S>(value: &[u8], serializer: S) -> Result<S::Ok, S::Error>
    where
        S: Serializer,
    {
        STANDARD.encode(value).serialize(serializer)
    }

    pub fn deserialize<'de, D>(deserializer: D) -> Result<Vec<u8>, D::Error>
    where
        D: Deserializer<'de>,
    {
        let encoded = String::deserialize(deserializer)?;
        STANDARD.decode(encoded).map_err(serde::de::Error::custom)
    }
}
