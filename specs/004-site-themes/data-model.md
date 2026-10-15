# Appearance state

| State | Valid values | Source / lifetime |
| --- | --- | --- |
| Selection | system, light, dark | Native select, current visit |
| Override | light, dark, absent | Root data-theme attribute |
| Saved override | light, dark, absent | LocalStorage key sourcesteward.theme |

Invalid persisted values map to system. Selecting system removes the saved override and root attribute. Selecting light/dark sets both, unless storage throws; then the visit still uses the selected override. System changes affect CSS only when no explicit override exists. No cross-tab synchronization contract is added.
