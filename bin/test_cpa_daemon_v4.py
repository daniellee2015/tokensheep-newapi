import importlib.util
import json
import math
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch


spec = importlib.util.spec_from_file_location(
    "cpa_daemon", Path(__file__).with_name("cpa-daemon-v4.py")
)
daemon = importlib.util.module_from_spec(spec)
spec.loader.exec_module(daemon)


class QuotaDecisionTests(unittest.TestCase):
    def test_tiny_nonzero_quota_is_not_displayed_as_zero(self):
        self.assertEqual(daemon.quota_fraction_pct(0.0), "0.0%")
        self.assertEqual(daemon.quota_fraction_pct(0.0004), "<0.1%")

    def test_quota_above_exhausted_threshold_stays_enabled(self):
        action, _ = daemon.decide(
            {"disabled": False}, {"gemini-weekly": {"frac": 0.06}}, None
        )
        self.assertEqual(action, "keep")

    def test_nonzero_weekly_bucket_is_not_disabled(self):
        action, _ = daemon.decide(
            {"disabled": False}, {"gemini-weekly": {"frac": 0.05}}, None
        )
        self.assertEqual(action, "keep")

    def test_auto_disabled_account_below_recovery_floor_stays_in_reserve(self):
        action, _ = daemon.decide(
            {"disabled": True},
            {"gemini-weekly": {"frac": 0.049}, "gemini-5h": {"frac": 0.049}},
            None,
            auto_disabled=True,
        )
        self.assertEqual(action, "keep")

    def test_manually_disabled_healthy_account_stays_disabled(self):
        action, _ = daemon.decide(
            {"disabled": True}, {"gemini-weekly": {"frac": 0.10}}, None
        )
        self.assertEqual(action, "keep")

    def test_daemon_disabled_account_rejoins_with_nonzero_buckets(self):
        action, _ = daemon.decide(
            {"disabled": True},
            {"gemini-weekly": {"frac": 0.10}, "gemini-5h": {"frac": 0.10}},
            None,
            auto_disabled=True,
        )
        self.assertEqual(action, "enable")

    def test_daemon_disabled_account_waits_for_both_recovery_floors(self):
        for buckets in (
            {"gemini-weekly": {"frac": 0.10}, "gemini-5h": {"frac": 0.049}},
            {"gemini-weekly": {"frac": 0.049}, "gemini-5h": {"frac": 0.10}},
        ):
            with self.subTest(buckets=buckets):
                action, _ = daemon.decide(
                    {"disabled": True}, buckets, None, auto_disabled=True
                )
                self.assertEqual(action, "keep")

    def test_daemon_disabled_account_waits_for_five_hour_bucket(self):
        blocked_buckets = (
            {"gemini-weekly": {"frac": 1.0}},
            {"gemini-weekly": {"frac": 1.0}, "gemini-5h": {"frac": 0.0}},
            {"gemini-weekly": {"frac": 0.0}, "gemini-5h": {"frac": 0.09}},
            {"gemini-weekly": {"frac": 1.0}, "gemini-5h": {"frac": "1"}},
        )
        for buckets in blocked_buckets:
            with self.subTest(buckets=buckets):
                action, _ = daemon.decide(
                    {"disabled": True}, buckets, None, auto_disabled=True
                )
                self.assertEqual(action, "keep")

    def test_true_zero_weekly_disables_even_with_five_hour_capacity(self):
        action, _ = daemon.decide(
            {"disabled": False},
            {"gemini-weekly": {"frac": 0.0}, "gemini-5h": {"frac": 0.98}},
            None,
            exhaustion_streaks={"gemini-weekly": 2},
        )
        self.assertEqual(action, "disable")

    def test_single_zero_quota_read_is_pending(self):
        action, reason = daemon.decide(
            {"disabled": False},
            {"gemini-weekly": {"frac": 0.0}, "gemini-5h": {"frac": 0.98}},
            None,
            exhaustion_streaks={"gemini-weekly": 1},
        )
        self.assertEqual(action, "keep")
        self.assertIn("pending", reason)

    def test_exhausted_five_hour_bucket_temporarily_disables_account(self):
        action, reason = daemon.decide(
            {"disabled": False},
            {"gemini-weekly": {"frac": 0.41}, "gemini-5h": {"frac": 0.0}},
            None,
            exhaustion_streaks={"gemini-5h": 2},
        )
        self.assertEqual(action, "disable")
        self.assertEqual(reason, "gemini-5h=0.0%")

    def test_nonzero_five_hour_bucket_uses_no_exhaustion_shutdown(self):
        action, _ = daemon.decide(
            {"disabled": False},
            {"gemini-weekly": {"frac": 0.41}, "gemini-5h": {"frac": 0.03}},
            None,
        )
        self.assertEqual(action, "keep")

    def test_failed_or_invalid_quota_does_not_change_account_status(self):
        for disabled in (False, True):
            for fraction in (None, -1, 2, "0", False, math.nan, math.inf):
                with self.subTest(disabled=disabled, fraction=fraction):
                    action, _ = daemon.decide(
                        {"disabled": disabled}, {"gemini-weekly": {"frac": fraction}}, None
                    )
                    self.assertEqual(action, "keep")
            for error in ("http429:rate limited", "http503:unavailable", "api-call-no-response"):
                action, _ = daemon.decide({"disabled": disabled}, {}, error)
                self.assertEqual(action, "keep")

    def test_missing_wrapper_status_does_not_mean_dead_credentials(self):
        self.assertFalse(daemon.looks_dead("httpNone:"))
        self.assertTrue(daemon.looks_dead("auth-refresh-failed"))

    def test_quota_disabled_state_survives_restart(self):
        with tempfile.TemporaryDirectory() as tmp_dir, patch.object(
            daemon, "AUTO_DISABLED_STATE_FILE", str(Path(tmp_dir) / "quota-disabled.json")
        ):
            daemon.save_quota_disabled({"b.json", "a.json"})
            self.assertEqual(daemon.load_quota_disabled(), {"a.json", "b.json"})

    def test_quota_target_defaults_to_stable_blue_process(self):
        with patch.dict(daemon.os.environ, {}, clear=True):
            self.assertEqual(daemon._resolve_cpa_url(), "http://cli-proxy-api-blue:8317")

    def test_explicit_quota_target_wins(self):
        with patch.dict(daemon.os.environ, {"CPA_URL": "http://blue:8317"}, clear=True):
            self.assertEqual(daemon._resolve_cpa_url(), "http://blue:8317")

    def test_quota_endpoint_matches_antigravity_request_host(self):
        self.assertEqual(
            daemon.QUOTA_URL,
            "https://daily-cloudcode-pa.googleapis.com/v1internal:retrieveUserQuotaSummary",
        )

    def test_quota_request_uses_account_project(self):
        response = json.dumps(
            {"status_code": 200, "body": json.dumps({"groups": []})}
        )
        with patch.object(daemon, "_run", return_value=response) as run:
            quota, error = daemon.fetch_quota("auth-index", "account-project")
        self.assertEqual(quota, {"groups": []})
        self.assertIsNone(error)
        command = run.call_args.args[0]
        payload = json.loads(command[command.index("-d") + 1])
        self.assertEqual(json.loads(payload["data"]), {"project": "account-project"})

    def test_quota_request_without_project_is_not_trusted(self):
        with patch.object(daemon, "_run") as run:
            quota, error = daemon.fetch_quota("auth-index", "")
        self.assertIsNone(quota)
        self.assertEqual(error, "missing-project-id")
        run.assert_not_called()

    def test_dry_run_does_not_advance_quarantine_streak(self):
        row = ("account", {"name": "account.json"}, {}, "auth-refresh-failed", "keep", "unreadable")
        with patch.object(daemon, "load_dead_streaks", return_value={}), patch.object(
            daemon, "save_dead_streaks"
        ) as save:
            daemon.apply_quarantine([row], False)
        save.assert_not_called()

    def test_failed_disable_does_not_free_an_enable_slot(self):
        auths = [
            {"email": "a", "name": "a.json", "auth_index": "a", "disabled": False},
            {"email": "b", "name": "b.json", "auth_index": "b", "disabled": True},
        ]
        with patch.object(daemon, "list_antigravity_auths", return_value=auths), patch.object(
            daemon, "fetch_quota", side_effect=[({}, None), ({}, None)]
        ), patch.object(daemon, "decide", side_effect=[("disable", "empty"), ("enable", "healthy")]), patch.object(
            daemon, "apply_quarantine", return_value=0
        ), patch.object(daemon, "load_quota_disabled", return_value=set()), patch.object(
            daemon, "save_quota_disabled"
        ), patch.object(daemon, "MAX_ACTIVE", 1), patch.object(
            daemon, "set_disabled", return_value=False
        ) as set_status, patch.object(daemon, "save_exhaustion_streaks"):
            daemon.run_cycle(True)
        set_status.assert_called_once_with("a.json", True)

    def test_successful_quota_disable_is_recorded_for_recovery(self):
        auth = {"email": "a", "name": "a.json", "auth_index": "a", "disabled": False}
        with patch.object(daemon, "list_antigravity_auths", return_value=[auth]), patch.object(
            daemon, "fetch_quota", return_value=({"groups": []}, None)
        ), patch.object(
            daemon,
            "extract_buckets",
            return_value={"gemini-weekly": {"frac": 0.0}},
        ), patch.object(daemon, "apply_quarantine", return_value=0), patch.object(
            daemon, "load_quota_disabled", return_value=set()
        ), patch.object(daemon, "save_quota_disabled") as save, patch.object(
            daemon, "set_disabled", return_value=True
        ) as set_status, patch.object(daemon, "EXHAUSTION_CONFIRMATIONS", 1), patch.object(
            daemon, "save_exhaustion_streaks"
        ):
            daemon.run_cycle(True)
        set_status.assert_called_once_with("a.json", True)
        save.assert_called_with({"a.json"})

    def test_successful_recovery_clears_quota_disabled_marker(self):
        auth = {"email": "a", "name": "a.json", "auth_index": "a", "disabled": True}
        with patch.object(daemon, "list_antigravity_auths", return_value=[auth]), patch.object(
            daemon, "fetch_quota", return_value=({"groups": []}, None)
        ), patch.object(
            daemon,
            "extract_buckets",
            return_value={"gemini-weekly": {"frac": 1.0}, "gemini-5h": {"frac": 1.0}},
        ), patch.object(daemon, "apply_quarantine", return_value=0), patch.object(
            daemon, "load_quota_disabled", return_value={"a.json"}
        ), patch.object(daemon, "save_quota_disabled") as save, patch.object(
            daemon, "set_disabled", return_value=True
        ) as set_status, patch.object(daemon, "save_exhaustion_streaks"):
            daemon.run_cycle(True)
        set_status.assert_called_once_with("a.json", False)
        save.assert_called_with(set())


if __name__ == "__main__":
    unittest.main()
