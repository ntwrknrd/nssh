use std::collections::BTreeSet;

pub fn inline_suggestion(prompt: &str, suggestions: &[String], selected: Option<usize>) -> String {
    let Some(prefix) = active_target_prefix(prompt) else {
        return String::new();
    };
    let suggestion = selected
        .and_then(|index| suggestions.get(index))
        .or_else(|| suggestions.first());
    suggestion
        .and_then(|value| completion_value(value))
        .and_then(|value| value.strip_prefix(prefix).map(str::to_string))
        .unwrap_or_default()
}

pub fn complete_prompt(prompt: &str, suggestion: &str) -> Option<String> {
    let span = active_target_span(prompt)?;
    let prefix = span.value;
    let value = completion_value(suggestion)?;
    if !value.starts_with(prefix) {
        return None;
    }
    let mut next = String::new();
    next.push_str(&prompt[..span.start]);
    next.push_str(&value);
    next.push_str(&prompt[span.end..]);
    Some(next)
}

pub fn complete_selected_prompts<I>(
    prompt: &str,
    suggestions: &[String],
    selected: I,
) -> Option<String>
where
    I: IntoIterator<Item = usize>,
{
    selected_prompt(prompt, suggestions, selected)
}

pub fn selected_prompt_preview<I>(
    prompt: &str,
    suggestions: &[String],
    selected: I,
) -> Option<String>
where
    I: IntoIterator<Item = usize>,
{
    selected_prompt(prompt, suggestions, selected)
}

fn selected_prompt<I>(prompt: &str, suggestions: &[String], selected: I) -> Option<String>
where
    I: IntoIterator<Item = usize>,
{
    let group = active_target_group(prompt)?;
    let span = active_target_span(prompt)?;
    let selected: BTreeSet<usize> = selected.into_iter().collect();
    let values: Vec<String> = selected
        .into_iter()
        .filter_map(|index| suggestions.get(index))
        .filter_map(|suggestion| completion_value(suggestion))
        .filter(|value| value.starts_with(span.value))
        .collect();
    if values.is_empty() {
        None
    } else {
        let targets = values
            .into_iter()
            .map(|value| format!("'{}'", value.replace('\'', "\\'")))
            .collect::<Vec<_>>()
            .join(", ");
        let mut next = String::new();
        next.push_str(&prompt[..group.start]);
        next.push(' ');
        next.push_str(&targets);
        next.push(' ');
        next.push_str(&prompt[group.end..]);
        Some(next)
    }
}

pub fn structural_complete_prompt(prompt: &str) -> Option<String> {
    let group = active_target_group(prompt)?;
    let target = active_target_span(prompt)?.value;
    if !target.contains('(') {
        return None;
    }
    let expanded = expand_host_pattern(target)?;
    let values: Vec<String> = expanded
        .into_iter()
        .filter(|host| !host.trim().is_empty())
        .map(|host| format!("'{}'", host.trim().replace('\'', "\\'")))
        .collect();
    if values.is_empty() {
        return None;
    }
    let mut next = String::new();
    next.push_str(&prompt[..group.start]);
    next.push(' ');
    next.push_str(&values.join(", "));
    next.push(' ');
    next.push_str(&prompt[group.end..]);
    if next == prompt {
        None
    } else {
        Some(next)
    }
}

fn expand_host_pattern(target: &str) -> Option<Vec<String>> {
    let Some(start) = target.find('(') else {
        return Some(vec![target.to_string()]);
    };
    let end = target.rfind(')')?;
    if end != target.len() - 1 || end < start {
        return None;
    }
    let prefix = &target[..start];
    let body = &target[start + 1..end];
    if prefix.is_empty() || body.trim().is_empty() {
        return None;
    }
    let mut hosts = Vec::new();
    for part in body.split(',') {
        let part = part.trim();
        if part.is_empty() {
            return None;
        }
        hosts.push(format!("{prefix}{part}"));
    }
    Some(hosts)
}

pub fn completion_value(suggestion: &str) -> Option<String> {
    let value = suggestion.trim();
    if value.is_empty() {
        return None;
    }
    Some(value.to_string())
}

pub fn active_target_prefix(prompt: &str) -> Option<&str> {
    active_target_span(prompt).map(|span| span.value)
}

#[derive(Debug, Clone, Copy)]
struct TargetGroup {
    start: usize,
    end: usize,
}

#[derive(Debug, Clone, Copy)]
struct TargetSpan<'a> {
    value: &'a str,
    start: usize,
    end: usize,
}

fn active_target_group(prompt: &str) -> Option<TargetGroup> {
    let value = prompt.trim_start();
    let leading = prompt.len() - value.len();
    if !value.starts_with('[') {
        return None;
    }
    let close = find_unquoted(value, ']')?;
    let command_tail = value[close + 1..].trim();
    if command_tail != "( '' )" {
        return None;
    }
    Some(TargetGroup {
        start: leading + 1,
        end: leading + close,
    })
}

fn active_target_span(prompt: &str) -> Option<TargetSpan<'_>> {
    let group = active_target_group(prompt)?;
    let body = &prompt[group.start..group.end];
    let body_trimmed = body.trim();
    let body_offset = body.find(body_trimmed).unwrap_or(0);
    if body_trimmed.matches('\'').count() != 2 {
        return None;
    }
    if !body_trimmed.starts_with('\'') || !body_trimmed.ends_with('\'') {
        return None;
    }
    let start = group.start + body_offset + 1;
    let end = group.start + body_offset + body_trimmed.len() - 1;
    Some(TargetSpan {
        value: &prompt[start..end],
        start,
        end,
    })
}

fn find_unquoted(value: &str, needle: char) -> Option<usize> {
    let mut in_quote = false;
    let mut escaped = false;
    for (index, ch) in value.char_indices() {
        if escaped {
            escaped = false;
            continue;
        }
        if in_quote && ch == '\\' {
            escaped = true;
            continue;
        }
        if ch == '\'' {
            in_quote = !in_quote;
            continue;
        }
        if !in_quote && ch == needle {
            return Some(index);
        }
    }
    None
}

#[derive(Debug, Clone, Default, PartialEq, Eq)]
pub struct CompletionPicker {
    pub open: bool,
    pub selected: usize,
    checked: BTreeSet<usize>,
}

impl CompletionPicker {
    pub fn open(&mut self) {
        self.open = true;
        self.selected = 0;
    }

    pub fn close(&mut self) {
        self.open = false;
        self.selected = 0;
        self.checked.clear();
    }

    pub fn next(&mut self, len: usize) {
        if len > 0 {
            self.selected = (self.selected + 1) % len;
        }
    }

    pub fn previous(&mut self, len: usize) {
        if len > 0 {
            self.selected = if self.selected == 0 {
                len - 1
            } else {
                self.selected - 1
            };
        }
    }

    pub fn selected_index(&self, len: usize) -> Option<usize> {
        if self.open && self.selected < len {
            Some(self.selected)
        } else {
            None
        }
    }

    pub fn toggle_selected(&mut self, len: usize) {
        if self.selected >= len {
            return;
        }
        if !self.checked.insert(self.selected) {
            self.checked.remove(&self.selected);
        }
    }

    pub fn is_checked(&self, index: usize) -> bool {
        self.checked.contains(&index)
    }

    pub fn checked_indices(&self) -> impl Iterator<Item = usize> + '_ {
        self.checked.iter().copied()
    }

    pub fn clamp(&mut self, len: usize) {
        if len == 0 {
            self.close();
            return;
        }
        if self.selected >= len {
            self.selected = 0;
        }
        self.checked.retain(|index| *index < len);
    }
}
