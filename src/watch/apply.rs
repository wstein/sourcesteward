//! `State::apply`: the one place where an event changes the state (stub).
use super::event::Event;
use super::state::State;

impl State {
    /// Applies the transition of `ev`.
    pub fn apply(&mut self, ev: &Event) {
        let _ = ev;
    }
}
