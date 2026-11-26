//! Which units the platform knows.
//!
//! Device identity is the gateway's business: a frame naming a unit nobody has
//! heard of is refused here rather than being carried into the platform and
//! sorted out later. The registry is read-only — the gateway never writes to
//! the operational database.

use std::collections::HashSet;
use std::sync::{Arc, RwLock};
use std::time::Duration;

use tokio_postgres::NoTls;

/// The set of unit ids the platform knows, refreshed in the background.
///
/// Until the first successful load the registry is empty and reports itself as
/// not ready: refusing frames it cannot validate is safer than accepting them
/// on trust.
#[derive(Debug, Clone, Default)]
pub struct UnitRegistry {
    units: Arc<RwLock<Option<HashSet<String>>>>,
}

impl UnitRegistry {
    /// An empty registry that is not yet ready.
    #[must_use]
    pub fn new() -> Self {
        Self::default()
    }

    /// Whether a unit is known. Unknown while the registry has never loaded,
    /// which the readiness probe reports separately.
    #[must_use]
    pub fn contains(&self, unit_id: &str) -> bool {
        self.units
            .read()
            .ok()
            .and_then(|guard| guard.as_ref().map(|units| units.contains(unit_id)))
            .unwrap_or(false)
    }

    /// Whether the registry has ever been loaded.
    #[must_use]
    pub fn is_ready(&self) -> bool {
        self.units
            .read()
            .ok()
            .map(|guard| guard.is_some())
            .unwrap_or(false)
    }

    /// How many units are known, for the gateway's own logging.
    #[must_use]
    pub fn len(&self) -> usize {
        self.units
            .read()
            .ok()
            .and_then(|guard| guard.as_ref().map(HashSet::len))
            .unwrap_or(0)
    }

    /// Whether the registry holds no units.
    #[must_use]
    pub fn is_empty(&self) -> bool {
        self.len() == 0
    }

    /// Replaces the known set.
    pub fn replace(&self, units: HashSet<String>) {
        if let Ok(mut guard) = self.units.write() {
            *guard = Some(units);
        }
    }

    /// Loads the registry once.
    pub async fn load(&self, dsn: &str) -> Result<usize, String> {
        let (client, connection) = tokio_postgres::connect(dsn, NoTls)
            .await
            .map_err(|error| format!("connect to postgres: {error}"))?;

        // The connection drives the protocol and has to be polled for the
        // client to work; it ends when the client is dropped.
        tokio::spawn(async move {
            if let Err(error) = connection.await {
                tracing::warn!(error = %error, "postgres connection closed");
            }
        });

        let rows = client
            .query("SELECT id::text FROM patrol_units", &[])
            .await
            .map_err(|error| format!("query patrol units: {error}"))?;

        let units: HashSet<String> = rows.iter().map(|row| row.get::<_, String>(0)).collect();
        let count = units.len();
        self.replace(units);

        Ok(count)
    }

    /// Keeps the registry current in the background. A failed refresh leaves
    /// the previous set in place: a database hiccup must not start refusing
    /// every frame from units that plainly exist.
    pub fn refresh_in_background(&self, dsn: String, every: Duration) {
        let registry = self.clone();
        tokio::spawn(async move {
            loop {
                match registry.load(&dsn).await {
                    Ok(count) => tracing::debug!(units = count, "unit registry refreshed"),
                    Err(error) => tracing::warn!(error = %error, "unit registry refresh failed"),
                }
                tokio::time::sleep(every).await;
            }
        });
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_registry_that_never_loaded_is_not_ready() {
        let registry = UnitRegistry::new();

        assert!(!registry.is_ready());
        assert!(!registry.contains("unit-1"));
        assert!(registry.is_empty());
    }

    #[test]
    fn a_loaded_registry_answers_for_its_units() {
        let registry = UnitRegistry::new();
        registry.replace(HashSet::from(["unit-1".to_owned(), "unit-2".to_owned()]));

        assert!(registry.is_ready());
        assert!(registry.contains("unit-1"));
        assert!(!registry.contains("unit-9"));
        assert_eq!(registry.len(), 2);
    }

    #[test]
    fn an_empty_site_is_still_a_loaded_registry() {
        // A site with no units is a real state, and it is not the same as
        // never having read the registry.
        let registry = UnitRegistry::new();
        registry.replace(HashSet::new());

        assert!(registry.is_ready());
        assert!(registry.is_empty());
    }
}
