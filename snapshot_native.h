// Included after the binding's native wrappers and helpers are defined.
// All snapshot bytes belong to this private trusted-artifact API.
extern "C" {

uint32_t SnapshotCompatibilityTag() {
  return ScriptCompiler::CachedDataVersionTag();
}

RtnSnapshot CreateLibrarySnapshot(const char *source, char **names, int count) {
  RtnSnapshot rtn = {};
  SnapshotCreator creator;
  Isolate *iso = creator.GetIsolate();
  {
    HandleScope handles(iso);
    Local<Context> clean = Context::New(iso);
    creator.SetDefaultContext(clean);
    Local<Context> library = Context::New(iso);
    Context::Scope scope(library);
    TryCatch caught(iso);
    Local<String> text;
    Local<Script> script;
    Local<Value> result;
    if (!String::NewFromUtf8(iso, source).ToLocal(&text) ||
        !Script::Compile(library, text).ToLocal(&script) ||
        !script->Run(library).ToLocal(&result)) {
      rtn.error = ExceptionError(caught, iso, library);
      return rtn;
    }
    for (int i = 0; i < count; ++i) {
      Local<String> name;
      Local<Value> value;
      if (!String::NewFromUtf8(iso, names[i]).ToLocal(&name) ||
          !library->Global()->Get(library, name).ToLocal(&value)) {
        rtn.error = ExceptionError(caught, iso, library);
        return rtn;
      }
      if (value->IsUndefined()) {
        rtn.error.msg = CopyString("snapshot export is missing or undefined");
        return rtn;
      }
      creator.AddData(library, value);
      if (!library->Global()->Delete(library, name).FromMaybe(false)) {
        rtn.error.msg = CopyString("snapshot export global is not deletable");
        return rtn;
      }
    }
    creator.AddContext(library);
  }
  // V8 requires all local handles gone before CreateBlob. The creator owns its
  // isolate; the returned byte array belongs to the caller, not that isolate.
  StartupData blob =
      creator.CreateBlob(SnapshotCreator::FunctionCodeHandling::kClear);
  rtn.data = const_cast<char *>(blob.data);
  rtn.length = blob.raw_size;
  return rtn;
}

void FreeLibrarySnapshot(char *data) { delete[] data; }

IsolatePtr NewSnapshotIsolate(const char *data, int length) {
  char *owned = new char[length];
  memcpy(owned, data, length);
  StartupData *snapshot = new StartupData{owned, length};
  if (!snapshot->IsValid()) {
    delete[] owned;
    delete snapshot;
    return nullptr;
  }
  Isolate::CreateParams params;
  params.array_buffer_allocator = default_allocator;
  params.snapshot_blob = snapshot;
  Isolate *iso = Isolate::New(params);
  Locker locker(iso);
  Isolate::Scope isolate_scope(iso);
  HandleScope handles(iso);
  iso->SetCaptureStackTraceForUncaughtExceptions(true);
  m_ctx *internal = new m_ctx{};
  internal->iso = iso;
  internal->snapshot = snapshot;
  // Discover the runtime's insertion boundary once, using the internal context
  // already required by v8go. There are no Go callbacks in this probe template.
  Local<String> marker =
      String::NewFromUtf8Literal(iso, "__v8go_snapshot_late_probe__");
  Local<ObjectTemplate> probe = ObjectTemplate::New(iso);
  probe->Set(marker, True(iso));
  Local<Context> context = Context::New(iso, nullptr, probe);
  Context::Scope context_scope(context);
  Local<Array> keys =
      context->Global()
          ->GetOwnPropertyNames(context, ALL_PROPERTIES,
                                KeyConversionMode::kConvertToString)
          .ToLocalChecked();
  bool after_marker = false;
  for (uint32_t i = 0; i < keys->Length(); ++i) {
    Local<Value> key = keys->Get(context, i).ToLocalChecked();
    if (key->StrictEquals(marker))
      after_marker = true;
    else if (after_marker)
      internal->late_globals.emplace_back(iso, key.As<Name>());
  }
  context->Global()->Delete(context, marker).Check();
  // Runtime builtins need not all honor template overrides (V8 15's Atomics
  // does not). Probe each key with a primitive sentinel, retaining only names,
  // never values or callback functions belonging to the temporary realms.
  for (uint32_t i = 0; i < keys->Length(); ++i) {
    Local<Name> key = keys->Get(context, i).ToLocalChecked().As<Name>();
    if (key->StrictEquals(marker)) continue;
    Local<ObjectTemplate> collision = ObjectTemplate::New(iso);
    collision->Set(key, marker);
    Local<Context> trial = Context::New(iso, nullptr, collision);
    Context::Scope trial_scope(trial);
    Local<Value> actual = trial->Global()->Get(trial, key).ToLocalChecked();
    if (!actual->StrictEquals(marker))
      internal->protected_globals.emplace_back(iso, key);
  }
  internal->ptr.Reset(iso, context);
  iso->SetData(0, internal);
  return iso;
}

// Copy descriptors instead of assignment so ReadOnly/DontEnum/DontDelete,
// accessors and symbols survive. Instantiate in the restored context itself:
// callback functions must never retain a throwaway context as their home realm.
static bool CopySnapshotProperty(Isolate *iso, Local<Context> context,
                                 Local<Object> instance, Local<Object> target,
                                 Local<Name> key) {
  Local<Value> descriptor_value;
  if (!instance->GetOwnPropertyDescriptor(context, key)
           .ToLocal(&descriptor_value) ||
      !descriptor_value->IsObject())
    return false;
  Local<Object> descriptor = descriptor_value.As<Object>();
  auto field = [&](const char *name) {
    return String::NewFromUtf8(iso, name).ToLocalChecked();
  };
  Local<Value> enumerable, configurable;
  if (!descriptor->Get(context, field("enumerable")).ToLocal(&enumerable) ||
      !descriptor->Get(context, field("configurable")).ToLocal(&configurable))
    return false;
  if (descriptor->HasOwnProperty(context, field("value")).FromMaybe(false)) {
    Local<Value> value, writable;
    if (!descriptor->Get(context, field("value")).ToLocal(&value) ||
        !descriptor->Get(context, field("writable")).ToLocal(&writable))
      return false;
    PropertyDescriptor copy(value, writable->BooleanValue(iso));
    copy.set_enumerable(enumerable->BooleanValue(iso));
    copy.set_configurable(configurable->BooleanValue(iso));
    return target->DefineProperty(context, key, copy).FromMaybe(false);
  }
  Local<Value> get, set;
  if (!descriptor->Get(context, field("get")).ToLocal(&get) ||
      !descriptor->Get(context, field("set")).ToLocal(&set))
    return false;
  PropertyDescriptor copy(get, set);
  copy.set_enumerable(enumerable->BooleanValue(iso));
  copy.set_configurable(configurable->BooleanValue(iso));
  return target->DefineProperty(context, key, copy).FromMaybe(false);
}

static bool CopySnapshotGlobals(Isolate *iso, Local<Context> context,
                                Local<Object> instance,
                                bool skip_existing = false,
                                Local<Object> protected_values = Local<Object>()) {
  Local<Array> keys;
  if (!instance
           ->GetOwnPropertyNames(context, ALL_PROPERTIES,
                                 KeyConversionMode::kConvertToString)
           .ToLocal(&keys))
    return false;
  for (uint32_t i = 0; i < keys->Length(); ++i) {
    Local<Value> key;
    if (!keys->Get(context, i).ToLocal(&key) || !key->IsName())
      return false;
    if (!protected_values.IsEmpty()) {
      m_ctx *internal = static_cast<m_ctx *>(iso->GetData(0));
      bool protected_key = false;
      for (auto &saved : internal->protected_globals) {
        if (key->StrictEquals(saved.Get(iso))) {
          protected_key = true;
          break;
        }
      }
      if (protected_key) {
        // A late builtin may overwrite a template value but retain its slot
        // in property order (e.g. Temporal). Restore its own-realm descriptor
        // at that slot; early builtins are already in their original slot.
        if (protected_values->HasOwnProperty(context, key.As<Name>()).FromMaybe(false) &&
            !CopySnapshotProperty(iso, context, protected_values,
                                  context->Global(), key.As<Name>()))
          return false;
        continue;
      }
    }
    if (skip_existing) {
      Maybe<bool> has =
          context->Global()->HasOwnProperty(context, key.As<Name>());
      if (has.IsNothing())
        return false;
      if (has.FromJust())
        continue;
    }
    if (!CopySnapshotProperty(iso, context, instance, context->Global(),
                              key.As<Name>()))
      return false;
  }
  return true;
}

static bool InstallSnapshotGlobals(Isolate *iso, Local<Context> context,
                                   Local<Object> instance) {
  Local<Object> late = Object::New(iso);
  m_ctx *internal = static_cast<m_ctx *>(iso->GetData(0));
  // Restore exact insertion order without replacing constructor values captured
  // by the library. Runtime probing determines template/builtin precedence.
  for (auto &saved : internal->late_globals) {
    Local<Name> key = saved.Get(iso);
    Maybe<bool> has = context->Global()->HasOwnProperty(context, key);
    if (has.IsNothing())
      return false;
    if (!has.FromJust())
      continue;
    if (!CopySnapshotProperty(iso, context, context->Global(), late, key) ||
        !context->Global()->Delete(context, key).FromMaybe(false))
      return false;
  }
  return CopySnapshotGlobals(iso, context, instance, false, late) &&
         CopySnapshotGlobals(iso, context, late, true);
}

RtnSnapshotContext NewLibraryContext(IsolatePtr iso, TemplatePtr global,
                                     int ref) {
  RtnSnapshotContext rtn = {};
  Locker locker(iso);
  Isolate::Scope isolate_scope(iso);
  HandleScope handles(iso);
  TryCatch caught(iso);
  Local<Context> context;
  if (!Context::FromSnapshot(iso, 0).ToLocal(&context)) {
    rtn.error.msg = CopyString("V8 did not restore library context");
    return rtn;
  }
  Context::Scope context_scope(context);
  context->SetEmbedderData(1, Integer::New(iso, ref));
  if (global != nullptr) {
    Local<Object> instance;
    if (!global->ptr.Get(iso)
             .As<ObjectTemplate>()
             ->NewInstance(context)
             .ToLocal(&instance) ||
        !InstallSnapshotGlobals(iso, context, instance)) {
      if (caught.HasCaught())
        rtn.error = ExceptionError(caught, iso, context);
      else
        rtn.error.msg = CopyString("cannot copy global template descriptors");
      return rtn;
    }
  }
  m_ctx *ctx = new m_ctx{};
  ctx->iso = iso;
  ctx->ptr.Reset(iso, context);
  context->SetAlignedPointerInEmbedderData(2, ctx,
                                          kEmbedderDataTypeTagDefault);
  rtn.ptr = ctx;
  return rtn;
}

RtnValue LibraryContextData(ContextPtr ctx, int index) {
  RtnValue rtn = {};
  Isolate *iso = ctx->iso;
  Locker locker(iso);
  Isolate::Scope isolate_scope(iso);
  HandleScope handles(iso);
  Local<Context> context = ctx->ptr.Get(iso);
  Context::Scope context_scope(context);
  Local<Value> data;
  if (!context->GetDataFromSnapshotOnce<Value>(index).ToLocal(&data)) {
    rtn.error.msg = CopyString("snapshot data is missing or already retrieved");
    return rtn;
  }
  m_value *value = new m_value{};
  value->iso = iso;
  value->ctx = ctx;
  value->ptr.Reset(iso, data);
  rtn.value = tracked_value(ctx, value);
  return rtn;
}

} // extern "C"
