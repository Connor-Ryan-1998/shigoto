// The property page's heading, shared by its layout (which loads it) and
// the Details tab (which renames the property), keyed by "org/id" so a
// stale name never shows on another property. strata says whether the
// property is linked to a strata scheme, which shows the Strata tab.
export const propertyHeader = $state({ key: '', name: '', strata: false });
