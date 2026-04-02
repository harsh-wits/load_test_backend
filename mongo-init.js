// MongoDB init script:
// - Uses root credentials from MONGO_INITDB_ROOT_* env vars
// - Creates/updates an app user for the configured application DB
//
// This runs only when the Mongo data directory is initialized (i.e., first startup
// for a fresh `mongo-data` volume).

(function () {
  const rootUser = process.env.MONGO_INITDB_ROOT_USERNAME;
  const rootPwd = process.env.MONGO_INITDB_ROOT_PASSWORD;

  const appDbName = process.env.MONGO_APP_DB || "load_tester";
  const appUser = process.env.MONGO_APP_USERNAME;
  const appPwd = process.env.MONGO_APP_PASSWORD;

  if (!rootUser || !rootPwd) {
    throw new Error("Missing MONGO_INITDB_ROOT_USERNAME / MONGO_INITDB_ROOT_PASSWORD");
  }
  if (!appUser || !appPwd) {
    throw new Error("Missing MONGO_APP_USERNAME / MONGO_APP_PASSWORD");
  }

  const adminDb = db.getSiblingDB("admin");
  adminDb.auth(rootUser, rootPwd);

  const appDb = db.getSiblingDB(appDbName);
  const existing = appDb.getUser(appUser);

  if (!existing) {
    appDb.createUser({
      user: appUser,
      pwd: appPwd,
      roles: [{ role: "readWrite", db: appDbName }],
    });
    return;
  }

  // Ensure password and roles stay in sync with config.
  appDb.updateUser(appUser, {
    pwd: appPwd,
    roles: [{ role: "readWrite", db: appDbName }],
  });
})();

