use nssh_repl_ratatui::completion::{
    active_target_prefix, complete_prompt, complete_selected_prompts, completion_value,
    inline_suggestion, selected_prompt_preview, structural_complete_prompt, CompletionPicker,
};

#[test]
fn inline_suggestion_returns_suffix_for_at_target_prefix() {
    let suggestions = vec!["acm-lab-agg-sw1".to_string()];

    assert_eq!(
        inline_suggestion("[ 'acm-lab' ] ( '' )", &suggestions, None),
        "-agg-sw1"
    );
}

#[test]
fn inline_suggestion_uses_selected_picker_item() {
    let suggestions = vec![
        "acm-lab-agg-sw1".to_string(),
        "acm-lab-border-sw1".to_string(),
    ];

    assert_eq!(
        inline_suggestion("[ 'acm-lab' ] ( '' )", &suggestions, Some(1)),
        "-border-sw1"
    );
}

#[test]
fn inline_suggestion_ignores_non_target_or_command_text() {
    let suggestions = vec!["edge01".to_string()];

    assert_eq!(inline_suggestion("edge", &suggestions, None), "");
    assert_eq!(
        inline_suggestion("[ 'edge01' ] ( 'show' )", &suggestions, None),
        ""
    );
}

#[test]
fn complete_prompt_accepts_suggestion_values() {
    assert_eq!(
        complete_prompt("[ 'ed' ] ( '' )", "edge01").as_deref(),
        Some("[ 'edge01' ] ( '' )")
    );
    assert_eq!(complete_prompt("[ 'sp' ] ( '' )", "edge01"), None);
}

#[test]
fn complete_selected_prompts_joins_checked_matching_targets() {
    let suggestions = vec![
        "acm-agg-sw1".to_string(),
        "acm-agg-sw2".to_string(),
        "edge01".to_string(),
    ];

    assert_eq!(
        complete_selected_prompts("[ 'acm' ] ( '' )", &suggestions, [1, 0]),
        Some("[ 'acm-agg-sw1', 'acm-agg-sw2' ] ( '' )".to_string())
    );
}

#[test]
fn selected_prompt_preview_joins_checked_targets_without_accepting_picker() {
    let suggestions = vec![
        "acm-agg-sw1".to_string(),
        "acm-agg-sw2".to_string(),
        "edge01".to_string(),
    ];

    assert_eq!(
        selected_prompt_preview("[ 'acm' ] ( '' )", &suggestions, [1, 0]),
        Some("[ 'acm-agg-sw1', 'acm-agg-sw2' ] ( '' )".to_string())
    );
    assert_eq!(
        selected_prompt_preview("[ 'acm' ] ( '' )", &suggestions, []),
        None
    );
}

#[test]
fn structural_completion_expands_host_pattern_targets() {
    assert_eq!(
        structural_complete_prompt("[ 'acm-lab-agg-sw(1,2)' ] ( '' )").as_deref(),
        Some("[ 'acm-lab-agg-sw1', 'acm-lab-agg-sw2' ] ( '' )")
    );
}

#[test]
fn structural_completion_closes_target_group() {
    assert_eq!(structural_complete_prompt("[ 'edge99' ] ( '' )"), None);
}

#[test]
fn completion_value_normalizes_bare_hosts() {
    assert_eq!(completion_value("edge01").as_deref(), Some("edge01"));
}

#[test]
fn completion_picker_wraps_selection() {
    let mut picker = CompletionPicker::default();

    picker.open();
    picker.previous(3);
    assert_eq!(picker.selected, 2);
    picker.next(3);
    assert_eq!(picker.selected, 0);
}

#[test]
fn completion_picker_toggles_and_clamps_checked_items() {
    let mut picker = CompletionPicker::default();

    picker.open();
    picker.toggle_selected(4);
    picker.next(4);
    picker.toggle_selected(4);
    assert!(picker.is_checked(0));
    assert!(picker.is_checked(1));

    picker.clamp(1);
    assert!(picker.is_checked(0));
    assert!(!picker.is_checked(1));

    picker.close();
    assert!(!picker.is_checked(0));
}

#[test]
fn active_target_prefix_only_matches_first_target_token() {
    assert_eq!(active_target_prefix("[ 'edge' ] ( '' )"), Some("edge"));
    assert_eq!(active_target_prefix("[ 'edge' ] ( 'show hostname' )"), None);
    assert_eq!(active_target_prefix("edge"), None);
}
