use crate::protocol::BrokerEvent;

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct App {
    prompt: String,
    history: Vec<String>,
    suggestions: Vec<String>,
    results: Vec<ResultBlock>,
    status: BrokerStatus,
    ready: bool,
    active: bool,
    message: String,
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct BrokerStatus {
    pub running: usize,
    pub done: usize,
    pub failed: usize,
    pub pending: usize,
    pub total: usize,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ResultBlock {
    pub batch: usize,
    pub index: usize,
    pub host: String,
    pub command: String,
    pub output: String,
    pub exit_code: i32,
    pub error: String,
}

impl App {
    pub fn apply(&mut self, event: BrokerEvent) {
        match event {
            BrokerEvent::Ready => {
                self.ready = true;
            }
            BrokerEvent::Suggestions { suggestions } => {
                self.suggestions = suggestions;
            }
            BrokerEvent::Started { .. } => {
                self.active = true;
            }
            BrokerEvent::Completed {
                batch,
                index,
                host,
                command,
                stdout,
                exit_code,
                error,
            } => {
                self.results.push(ResultBlock {
                    batch,
                    index,
                    host,
                    command,
                    output: String::from_utf8_lossy(&stdout).into_owned(),
                    exit_code,
                    error,
                });
                self.results
                    .sort_by_key(|result| (result.batch, result.index));
            }
            BrokerEvent::Status {
                running,
                done,
                failed,
                pending,
                total,
            } => {
                self.status = BrokerStatus {
                    running,
                    done,
                    failed,
                    pending,
                    total,
                };
                self.active = running > 0 || pending > 0;
            }
            BrokerEvent::History { lines } => {
                self.history = lines;
            }
            BrokerEvent::Error { message } => {
                self.message = message;
            }
        }
    }

    pub fn set_prompt(&mut self, value: String) {
        self.prompt = value;
    }

    pub fn prompt(&self) -> &str {
        &self.prompt
    }

    pub fn history(&self) -> &[String] {
        &self.history
    }

    pub fn suggestions(&self) -> &[String] {
        &self.suggestions
    }

    pub fn results(&self) -> &[ResultBlock] {
        &self.results
    }

    pub fn status(&self) -> BrokerStatus {
        self.status.clone()
    }

    pub fn status_line(&self) -> String {
        format!(
            "running {}/{}  done {}/{}  failed {}/{}  pending {}/{}{}",
            self.status.running,
            self.status.total,
            self.status.done,
            self.status.total,
            self.status.failed,
            self.status.total,
            self.status.pending,
            self.status.total,
            if self.message.is_empty() {
                String::new()
            } else {
                format!("  {}", self.message)
            }
        )
    }
}
