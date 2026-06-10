use crossterm::event::{Event, KeyCode, KeyEvent, KeyModifiers, MouseEvent, MouseEventKind};
use nssh_repl_ratatui::input::{
    apply_scroll_delta, apply_scroll_delta_clamped, capped_event_budget, scroll_delta, ScrollDelta,
};

#[test]
fn scroll_delta_coalesces_many_mouse_wheel_events() {
    let events = vec![
        scroll_down(),
        scroll_down(),
        scroll_up(),
        Event::Key(KeyEvent::new(KeyCode::Char('x'), KeyModifiers::NONE)),
    ];

    assert_eq!(scroll_delta(&events), ScrollDelta { up: 1, down: 2 });
}

#[test]
fn apply_scroll_delta_saturates_upward_and_combines_net_delta() {
    assert_eq!(apply_scroll_delta(0, ScrollDelta { up: 3, down: 0 }), 0);
    assert_eq!(apply_scroll_delta(10, ScrollDelta { up: 4, down: 1 }), 7);
    assert_eq!(apply_scroll_delta(10, ScrollDelta { up: 1, down: 4 }), 13);
}

#[test]
fn apply_scroll_delta_clamped_does_not_grow_past_bottom() {
    assert_eq!(
        apply_scroll_delta_clamped(50, ScrollDelta { up: 0, down: 100 }, 50),
        50
    );
    assert_eq!(
        apply_scroll_delta_clamped(48, ScrollDelta { up: 0, down: 100 }, 50),
        50
    );
    assert_eq!(
        apply_scroll_delta_clamped(50, ScrollDelta { up: 3, down: 0 }, 50),
        47
    );
}

#[test]
fn capped_event_budget_leaves_room_for_next_frame() {
    assert_eq!(capped_event_budget(0, 128), 0);
    assert_eq!(capped_event_budget(32, 128), 32);
    assert_eq!(capped_event_budget(999, 128), 128);
}

fn scroll_down() -> Event {
    Event::Mouse(MouseEvent {
        kind: MouseEventKind::ScrollDown,
        column: 0,
        row: 0,
        modifiers: KeyModifiers::NONE,
    })
}

fn scroll_up() -> Event {
    Event::Mouse(MouseEvent {
        kind: MouseEventKind::ScrollUp,
        column: 0,
        row: 0,
        modifiers: KeyModifiers::NONE,
    })
}
