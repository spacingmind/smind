// Static config lives in app.json. This adds the Firebase google-services.json for the Android build when the builder supplies it (EAS file environment variable GOOGLE_SERVICES_JSON, which holds a PATH); without it the app still builds, it just cannot register for push (ADR-0040 H3: the Human owns the Firebase project and its files).
module.exports = ({ config }) => ({
  ...config,
  android: {
    ...config.android,
    ...(process.env.GOOGLE_SERVICES_JSON ? { googleServicesFile: process.env.GOOGLE_SERVICES_JSON } : {}),
  },
});
