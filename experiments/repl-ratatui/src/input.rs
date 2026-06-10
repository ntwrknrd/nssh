use crossterm::event::{Event, MouseEventKind};
use std::time::Duration;

pub const MAX_DRAINED_TERMINAL_EVENTS: usize = 128;

#[derive(Debug, Copy, Clone, Default, PartialEq, Eq)]
pub struct ScrollDelta {
    pub up: usize,
    pub down: usize,
}

impl ScrollDelta {
    pub fn net(self) -> isize {
        self.down as isize - self.up as isize
    }
}

pub fn scroll_delta(events: &[Event]) -> ScrollDelta {
    let mut delta = ScrollDelta::default();
    for event in events {
        if let Event::Mouse(mouse) = event {
            match mouse.kind {
                MouseEventKind::ScrollUp => delta.up += 1,
                MouseEventKind::ScrollDown => delta.down += 1,
                _ => {}
            }
        }
    }
    delta
}

pub fn apply_scroll_delta(scroll: usize, delta: ScrollDelta) -> usize {
    let net = delta.net();
    if net < 0 {
        scroll.saturating_sub(net.unsigned_abs())
    } else {
        scroll.saturating_add(net as usize)
    }
}

pub fn apply_scroll_delta_clamped(scroll: usize, delta: ScrollDelta, max_scroll: usize) -> usize {
    apply_scroll_delta(scroll, delta).min(max_scroll)
}

pub fn capped_event_budget(queued: usize, cap: usize) -> usize {
    queued.min(cap)
}

pub fn drain_terminal_events(first: Event) -> std::io::Result<Vec<Event>> {
    let mut events = vec![first];
    while events.len() < capped_event_budget(usize::MAX, MAX_DRAINED_TERMINAL_EVENTS)
        && crossterm::event::poll(Duration::ZERO)?
    {
        events.push(crossterm::event::read()?);
    }
    Ok(events)
}
