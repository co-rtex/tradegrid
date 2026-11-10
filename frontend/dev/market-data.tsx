// Separate Vite development entry; never imported by the production app.
if (import.meta.env.DEV) {
  void import('./market-data-preview').then(({ mountPreview }) => mountPreview())
}
